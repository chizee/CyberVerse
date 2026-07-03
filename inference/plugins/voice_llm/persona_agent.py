from __future__ import annotations

import asyncio
import json
import logging
import re
from dataclasses import replace
from typing import Any, AsyncIterator, Callable

from inference.core.registry import import_plugin_class
from inference.core.types import (
    PluginConfig,
    ToolCall,
    ToolDefinition,
    ToolResult,
    VoiceLLMInputEvent,
    VoiceLLMOutputEvent,
    VoiceLLMSessionConfig,
)
from inference.plugins.voice_llm.base import VoiceLLMPlugin
from inference.plugins.voice_llm.persona.runtime import LocalTaskRuntime
from inference.plugins.voice_llm.persona.supervisor import PendingSubAgentTask, PersonaSupervisor, SupervisorToolResult
from inference.rag import RAGEngine, RAGSearchRequest

logger = logging.getLogger(__name__)


PERSONA_TOOL_DEFINITIONS = [
    ToolDefinition(
        name="create_task",
        description="为搜索、调研、聚合或报告类请求创建 CyberVerse 后台任务。",
        parameters={
            "type": "object",
            "properties": {
                "description": {
                    "type": "string",
                    "description": "用自然语言描述需要后台 SubAgent 完成的任务。不要拆解工具、类型或执行参数。",
                },
            },
            "required": ["description"],
        },
    ),
    ToolDefinition(
        name="get_task_status",
        description="获取当前会话中最新活跃的 CyberVerse 后台任务状态。",
        parameters={"type": "object", "properties": {}},
    ),
    ToolDefinition(
        name="cancel_task",
        description="取消当前会话中最新活跃的 CyberVerse 后台任务。",
        parameters={"type": "object", "properties": {}},
    ),
    ToolDefinition(
        name="retrieve_character_knowledge",
        description="当用户询问当前角色的知识库、导入文档或人物生平事实时使用；先检索再回答。",
        parameters={
            "type": "object",
            "properties": {
                "query": {
                    "type": "string",
                    "description": "用于检索角色素材库的具体问题或关键词。",
                },
            },
            "required": ["query"],
        },
    ),
]

PERSONA_AGENT_INSTRUCTIONS = """你是 CyberVerse 数字人 PersonaAgent，直接通过语音和用户对话。
你需要直接判断并处理用户当前表达。
普通寒暄、问答和闲聊：直接自然回答。
表达不清或缺少必要信息：用一句自然追问澄清，不要调用工具。
搜索、查询热点、查询知乎热榜、调研、整理资料、生成报告、生成网页或需要较长后台处理：必须调用 create_task，不要只用口头承诺代替工具调用。
当用户已经给出明确可执行的指令时，不能再追问领域、方向、范围、偏好或“想看哪些方面”；直接调用 create_task 执行。
“看一下今天知乎热榜”“帮我查一下知乎新鲜事”“用知乎搜索一下宇树科技”这类请求已经足够明确，必须直接创建任务。
调用 create_task 时只填写 description，用自然语言描述后台任务；不要决定任务类型、标题或具体工具参数。
调用 create_task 后，最多用一句很短的确认，例如“好的，我去查。”不要做空泛等待播报、不要承诺很快返回结果、不要再追加问题。
询问后台任务进度：调用 get_task_status。
要求取消、停止、不用继续当前后台任务：调用 cancel_task。
询问当前角色的导入知识、文档内容、经历、生平或背景事实：必须先调用 retrieve_character_knowledge；如果没有检索结果，再说明资料库里没有找到相关信息。

"""

_AUTO_TASK_MARKERS = (
    "后台",
    "复杂任务",
    "长任务",
    "subagent",
    "子任务",
    "异步",
)
_AUTO_TASK_VERBS = (
    "执行",
    "处理",
    "整理",
    "调研",
    "研究",
    "生成",
    "搜索",
    "查询",
    "查找",
    "报告",
    "方案",
    "分析",
)
_TASK_STATUS_RE = re.compile(r"(进度|到哪一步|执行到哪|处理到哪|查得怎么样|任务状态|后台状态|完成了吗|做完了吗)")
_AUTO_TASK_EXPLICIT_REQUESTS = (
    "请后台执行",
    "后台执行复杂任务",
    "执行复杂任务",
    "帮我后台",
    "创建后台",
    "新建后台",
    "任务开始后",
)


class PersonaAgentPlugin(VoiceLLMPlugin):
    """Persona wrapper for an underlying realtime omni provider.

    The public gRPC wire shape remains the existing VoiceLLM stream. Native tool
    calls are consumed inside this wrapper and are never forwarded to Go or the UI.
    """

    name = "persona.persona"

    def __init__(self) -> None:
        self.model_provider = "doubao"
        self.model_plugin: VoiceLLMPlugin | None = None
        self.model_plugins: dict[str, VoiceLLMPlugin] = {}
        self.omni_config: dict[str, Any] = {}
        self.shared_config: dict[str, Any] = {}
        self._model_plugin_lock = asyncio.Lock()
        self.task_runtime: LocalTaskRuntime | None = None
        self.supervisor: PersonaSupervisor | None = None
        self.rag_engine: RAGEngine | None = None
        self.task_poll_interval_seconds = 1.0
        self.task_monitor_timeout_seconds = 1800.0

    async def initialize(self, config: PluginConfig) -> None:
        self.model_provider = str(config.params.get("model_provider") or "doubao").strip()
        if not self.model_provider or self.model_provider == "persona":
            raise ValueError("persona model_provider must reference a concrete omni provider")

        self.task_poll_interval_seconds = max(
            0.1,
            float(config.params.get("task_poll_interval_seconds") or self.task_poll_interval_seconds),
        )
        self.task_monitor_timeout_seconds = max(
            1.0,
            float(config.params.get("task_monitor_timeout_seconds") or self.task_monitor_timeout_seconds),
        )

        omni_config = config.shared.get("omni", {})
        if not isinstance(omni_config, dict):
            raise ValueError("persona provider requires shared omni config")
        self.shared_config = config.shared
        self.omni_config = omni_config
        self.model_plugin = await self._model_plugin_for_provider(self.model_provider)

        runtime_config = config.shared.get("runtime_config")
        if not isinstance(runtime_config, dict):
            runtime_config = {
                "inference": {
                    "llm": config.shared.get("llm", {}),
                    "persona_agent": config.params,
                }
            }
        self.rag_engine = RAGEngine(runtime_config)
        self.task_runtime = LocalTaskRuntime(
            runtime_config=runtime_config,
            max_active_tasks_per_session=int(config.params.get("max_active_tasks_per_session") or 3),
        )
        self.supervisor = PersonaSupervisor(
            runtime=self.task_runtime,
            task_poll_interval_seconds=self.task_poll_interval_seconds,
            task_monitor_timeout_seconds=self.task_monitor_timeout_seconds,
        )
        await self.supervisor.initialize()

    def _provider_from_session(self, session_config: VoiceLLMSessionConfig | None) -> str:
        provider = str(getattr(session_config, "provider", "") or "").strip()
        if not provider or provider == "persona":
            return self.model_provider
        return provider

    async def _model_plugin_for_provider(self, provider: str) -> VoiceLLMPlugin:
        provider = provider.strip()
        if not provider or provider == "persona":
            provider = self.model_provider
        async with self._model_plugin_lock:
            if provider in self.model_plugins:
                return self.model_plugins[provider]
            provider_conf = self.omni_config.get(provider)
            if not isinstance(provider_conf, dict):
                raise ValueError(f"persona model_provider {provider!r} is not configured")
            class_path = provider_conf.get("plugin_class")
            if not class_path:
                raise ValueError(f"persona model_provider {provider!r} has no plugin_class")

            plugin_cls = import_plugin_class(str(class_path))
            model_plugin = plugin_cls()
            params = {k: v for k, v in provider_conf.items() if k != "plugin_class"}
            await model_plugin.initialize(
                PluginConfig(
                    plugin_name=f"omni.{provider}",
                    params=params,
                    shared=self.shared_config,
                )
            )
            if not isinstance(model_plugin, VoiceLLMPlugin):
                raise TypeError(f"{class_path} is not a VoiceLLMPlugin")
            self.model_plugins[provider] = model_plugin
            if provider == self.model_provider:
                self.model_plugin = model_plugin
            return model_plugin

    async def _model_plugin_for_session(
        self,
        session_config: VoiceLLMSessionConfig | None,
    ) -> VoiceLLMPlugin:
        return await self._model_plugin_for_provider(self._provider_from_session(session_config))

    async def shutdown(self) -> None:
        for plugin in self.model_plugins.values():
            await plugin.shutdown()
        self.model_plugins.clear()
        self.model_plugin = None
        if self.supervisor is not None:
            await self.supervisor.shutdown()
            self.supervisor = None

    async def check_voice(self, session_config: VoiceLLMSessionConfig | None = None) -> None:
        model_plugin = await self._model_plugin_for_session(session_config)
        await model_plugin.check_voice(session_config)

    async def interrupt(self) -> None:
        for plugin in self.model_plugins.values():
            await plugin.interrupt()

    async def _retrieve_character_knowledge(
        self,
        call: ToolCall,
        session_config: VoiceLLMSessionConfig,
    ) -> SupervisorToolResult:
        query = self._clean_text((call.arguments or {}).get("query")) or self._clean_text(
            (call.arguments or {}).get("text")
        )
        if not query:
            return SupervisorToolResult(result={"ok": False, "results": [], "error": "query is required"})
        if not session_config.character_dir:
            return SupervisorToolResult(result={"ok": True, "results": [], "reason": "character_dir_missing"})
        if self.rag_engine is None:
            return SupervisorToolResult(result={"ok": False, "results": [], "error": "rag engine is not initialized"})

        results = await self.rag_engine.search(
            RAGSearchRequest(
                character_id=session_config.character_id,
                character_dir=session_config.character_dir,
                query=query,
            )
        )
        return SupervisorToolResult(
            result={
                "ok": True,
                "query": query,
                "results": [
                    {
                        "source_id": item.source_id,
                        "title": item.title,
                        "filename": item.filename,
                        "content": item.content,
                        "score": item.score,
                    }
                    for item in results
                ],
            }
        )

    @staticmethod
    def _format_rag_response_instructions(query: str, results: list[dict[str, Any]]) -> str:
        lines = [
            "请回答用户刚才的问题。下列内容来自当前角色素材库；如果与问题相关，必须优先依据这些素材回答；如果无关，请忽略它们。",
            f"用户问题：{query}",
            "【角色素材检索结果】",
        ]
        for idx, item in enumerate(results, 1):
            title = str(item.get("title") or item.get("filename") or f"素材{idx}").strip()
            content = str(item.get("content") or "").strip()
            if not content:
                continue
            lines.append(f"[{idx}] {title}\n{content}")
        lines.append("不要提到内部检索过程。不要编造素材中没有的事实。")
        return "\n\n".join(lines)

    async def _rag_response_instructions(
        self,
        text: str,
        session_config: VoiceLLMSessionConfig,
    ) -> str:
        result = await self._retrieve_character_knowledge(
            ToolCall(
                id="persona_rag_pre_response",
                name="retrieve_character_knowledge",
                arguments={"query": text},
            ),
            session_config,
        )
        results = result.result.get("results")
        if not isinstance(results, list) or not results:
            logger.info(
                "persona RAG pre-response no results session=%s query=%s",
                session_config.session_id or "",
                self._clip_text(text),
            )
            return ""
        logger.info(
            "persona RAG pre-response hit session=%s query=%s results=%d",
            session_config.session_id or "",
            self._clip_text(text),
            len(results),
        )
        return self._format_rag_response_instructions(text, results)

    async def _execute_tool(
        self,
        call: ToolCall,
        session_config: VoiceLLMSessionConfig,
    ) -> SupervisorToolResult:
        if call.name.strip() == "retrieve_character_knowledge":
            return await self._retrieve_character_knowledge(call, session_config)
        if self.supervisor is None:
            raise RuntimeError("persona supervisor is not initialized")
        return await self.supervisor.handle_tool_call(
            call,
            session_config.session_id,
            self._session_context(session_config),
        )

    @staticmethod
    def _clean_text(text: Any) -> str:
        return str(text or "").strip()

    @staticmethod
    def _session_context(session_config: VoiceLLMSessionConfig) -> dict[str, Any]:
        return {
            "session_id": session_config.session_id,
            "character_id": session_config.character_id,
            "character_dir": session_config.character_dir,
        }

    @staticmethod
    def _needs_space(left: str, right: str) -> bool:
        if not left or not right:
            return False
        return left[-1].isascii() and right[0].isascii() and left[-1].isalnum() and right[0].isalnum()

    @classmethod
    def _merge_text_segments(cls, segments: list[str]) -> str:
        merged = ""
        for segment in segments:
            text = cls._clean_text(segment)
            if not text:
                continue
            if not merged:
                merged = text
                continue
            if text in merged:
                continue
            if merged in text:
                merged = text
                continue
            separator = " " if cls._needs_space(merged, text) else ""
            merged = f"{merged}{separator}{text}"
        return merged.strip()

    @classmethod
    def _final_user_text(
        cls,
        call: ToolCall,
        turn_transcripts: list[str],
    ) -> str:
        args = call.arguments or {}
        tool_text = cls._clean_text(
            args.get("description")
            or args.get("user_request")
            or args.get("request")
            or args.get("text")
        )
        return tool_text or cls._merge_text_segments(turn_transcripts)

    @staticmethod
    def _has_assistant_output(event: VoiceLLMOutputEvent) -> bool:
        return bool(event.transcript or event.audio or event.is_final)

    @staticmethod
    def _clip_text(value: Any, limit: int = 180) -> str:
        text = str(value or "")
        if len(text) <= limit:
            return text
        return text[:limit] + "..."

    @classmethod
    def _tool_calls_for_log(cls, calls: list[ToolCall]) -> list[dict[str, Any]]:
        logged: list[dict[str, Any]] = []
        for call in calls:
            args = call.arguments or {}
            logged.append(
                {
                    "id": call.id,
                    "name": call.name,
                    "arguments": cls._clip_text(json.dumps(args, ensure_ascii=False, sort_keys=True)),
                }
            )
        return logged

    @classmethod
    def _should_auto_create_task(cls, text: str) -> bool:
        normalized = cls._clean_text(text).lower()
        if not normalized:
            return False
        if _TASK_STATUS_RE.search(normalized):
            has_explicit_create = any(phrase.lower() in normalized for phrase in _AUTO_TASK_EXPLICIT_REQUESTS)
            if not has_explicit_create:
                return False
        has_marker = any(marker.lower() in normalized for marker in _AUTO_TASK_MARKERS)
        has_verb = any(verb.lower() in normalized for verb in _AUTO_TASK_VERBS)
        if has_marker and has_verb:
            return True
        return False

    @classmethod
    def _should_auto_get_task_status(cls, text: str) -> bool:
        normalized = cls._clean_text(text).lower()
        if not normalized:
            return False
        return bool(_TASK_STATUS_RE.search(normalized))

    @staticmethod
    def _auto_create_task_instructions(result: dict[str, Any]) -> str:
        if not result.get("accepted"):
            return "后台任务没有成功创建。请用一句自然口语告诉用户稍后再试。不要编造任务状态。"
        return "\n".join(
            [
                "CyberVerse 已经创建后台任务，任务进度会通过聊天侧任务卡片展示。",
                "请用一句自然口语确认任务已经开始，告诉用户可以继续聊天。",
                "不要再次调用 create_task。不要朗读任务 ID、JSON 或内部字段。",
            ]
        )

    @staticmethod
    def _auto_task_status_instructions(result: dict[str, Any]) -> str:
        task = result.get("task")
        events = result.get("events")
        if not isinstance(task, dict):
            return "当前会话没有活跃后台任务。请用一句自然口语告诉用户现在没有正在执行的后台任务。不要编造进度。"
        event_lines: list[str] = []
        if isinstance(events, list):
            for event in events[-3:]:
                if not isinstance(event, dict):
                    continue
                message = str(event.get("message") or "").strip()
                event_type = str(event.get("event_type") or "").strip()
                progress = event.get("progress", task.get("progress", 0))
                event_lines.append(f"- {event_type}: {message} ({progress}%)")
        return "\n".join(
            [
                "请基于下面的真实后台任务状态回答用户的进度问题。",
                f"状态：{task.get('status')}",
                f"进度：{task.get('progress', 0)}%",
                f"当前说明：{task.get('result_summary') or ''}",
                "最近事件：",
                *(event_lines or ["- 暂无事件"]),
                "不要再次调用 get_task_status。不要编造不存在的进度。",
            ]
        )

    @classmethod
    def _model_event_kind(cls, event: VoiceLLMOutputEvent) -> str:
        if event.tool_calls:
            return "tool_call"
        if event.user_transcript:
            return "user_transcript"
        if event.barge_in:
            return "turn_started"
        if event.is_final:
            return "assistant_final"
        if event.transcript:
            return "assistant_delta"
        if event.audio is not None:
            return "audio_delta"
        return "event"

    @classmethod
    def _log_model_event(cls, session_id: str, event: VoiceLLMOutputEvent) -> None:
        kind = cls._model_event_kind(event)
        audio = event.audio
        fields: dict[str, Any] = {
            "question_id": event.question_id,
            "reply_id": event.reply_id,
            "is_final": event.is_final,
            "barge_in": event.barge_in,
        }
        if event.user_transcript:
            fields["user_transcript"] = cls._clip_text(event.user_transcript)
        if event.transcript:
            fields["transcript"] = cls._clip_text(event.transcript)
        if audio is not None:
            fields["audio"] = {
                "bytes": len(audio.data or b""),
                "sample_rate": audio.sample_rate,
                "is_final": audio.is_final,
            }
        if event.tool_calls:
            fields["tool_calls"] = cls._tool_calls_for_log(event.tool_calls)
        info_kinds = {"turn_started", "user_transcript", "tool_call", "assistant_final"}
        log = logger.info if kind in info_kinds else logger.debug
        log(
            "persona model event session=%s kind=%s fields=%s",
            session_id or "",
            kind,
            json.dumps(fields, ensure_ascii=False, sort_keys=True),
        )

    @staticmethod
    def _task_event_payload(task: dict[str, Any], event: dict[str, Any]) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "type": "task_event",
            "task_id": event.get("task_id") or task.get("id"),
            "session_id": task.get("session_id"),
            "seq": event.get("seq"),
            "event_type": event.get("event_type"),
            "status": event.get("status") or task.get("status"),
            "message": event.get("message") or "",
            "progress": event.get("progress", task.get("progress", 0)),
            "created_at": event.get("created_at"),
            "task": task,
        }
        event_payload = event.get("payload")
        if isinstance(event_payload, str):
            try:
                event_payload = json.loads(event_payload)
            except json.JSONDecodeError:
                event_payload = {}
        if isinstance(event_payload, dict) and event_payload:
            payload["payload"] = event_payload
        return payload

    @staticmethod
    def _drain_task_events(queue: asyncio.Queue[dict[str, Any]]) -> list[dict[str, Any]]:
        drained: list[dict[str, Any]] = []
        while True:
            try:
                drained.append(queue.get_nowait())
            except asyncio.QueueEmpty:
                return drained

    async def _run_async_task(
        self,
        pending: PendingSubAgentTask,
        injected: asyncio.Queue[VoiceLLMInputEvent],
    ) -> None:
        if self.supervisor is None:
            raise RuntimeError("persona supervisor is not initialized")
        prompt = await self.supervisor.run_pending_task(pending)
        await injected.put(VoiceLLMInputEvent(text=prompt))

    @staticmethod
    def _persona_system_prompt(session_config: VoiceLLMSessionConfig) -> str:
        prompt = (session_config.system_prompt or "").strip()
        if not prompt:
            return PERSONA_AGENT_INSTRUCTIONS
        return f"{PERSONA_AGENT_INSTRUCTIONS}\n\n角色设定：\n{prompt}"

    async def _merged_input_stream(
        self,
        input_stream: AsyncIterator[VoiceLLMInputEvent],
        injected: asyncio.Queue[VoiceLLMInputEvent],
        should_wait_for_injected: Callable[[], bool] | None = None,
    ) -> AsyncIterator[VoiceLLMInputEvent]:
        source = input_stream.__aiter__()
        source_done = False
        while True:
            try:
                while True:
                    yield injected.get_nowait()
            except asyncio.QueueEmpty:
                pass

            if source_done:
                try:
                    if should_wait_for_injected is not None and should_wait_for_injected():
                        yield await injected.get()
                    else:
                        yield await asyncio.wait_for(injected.get(), timeout=0.2)
                    continue
                except asyncio.TimeoutError:
                    return

            try:
                yield await source.__anext__()
            except StopAsyncIteration:
                source_done = True

    async def converse_stream(
        self,
        input_stream: AsyncIterator[VoiceLLMInputEvent],
        session_config: VoiceLLMSessionConfig | None = None,
    ) -> AsyncIterator[VoiceLLMOutputEvent]:
        session_config = session_config or VoiceLLMSessionConfig()
        model_plugin = await self._model_plugin_for_session(session_config)
        model_session_config = replace(
            session_config,
            system_prompt=self._persona_system_prompt(session_config),
            tools=PERSONA_TOOL_DEFINITIONS,
            defer_response=True,
        )
        injected: asyncio.Queue[VoiceLLMInputEvent] = asyncio.Queue()
        turn_transcripts: list[str] = []
        pending_task_starts: list[PendingSubAgentTask] = []
        auto_tool_results: dict[str, dict[str, Any]] = {}
        background_tasks: set[asyncio.Task[None]] = set()
        task_events: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        remove_task_event_listener = None

        def enqueue_task_event(task: dict[str, Any], event: dict[str, Any]) -> None:
            if str(task.get("session_id") or "") != str(session_config.session_id or ""):
                return
            task_events.put_nowait(self._task_event_payload(task, event))

        if hasattr(self.task_runtime, "add_event_listener"):
            remove_task_event_listener = self.task_runtime.add_event_listener(enqueue_task_event)  # type: ignore[union-attr]

        def schedule_task_start(pending: PendingSubAgentTask) -> None:
            task = asyncio.create_task(self._run_async_task(pending, injected))
            background_tasks.add(task)
            task.add_done_callback(background_tasks.discard)

        async def auto_route_user_text(user_text: str) -> tuple[str | None, bool]:
            if self._should_auto_create_task(user_text):
                tool_result = await self._execute_tool(
                    ToolCall(
                        id="persona_auto_create_task",
                        name="create_task",
                        arguments={"description": user_text},
                    ),
                    session_config,
                )
                auto_tool_results["create_task"] = tool_result.result
                if tool_result.pending_task is not None:
                    pending_task_starts.append(tool_result.pending_task)
                return self._auto_create_task_instructions(tool_result.result), True
            if self._should_auto_get_task_status(user_text):
                tool_result = await self._execute_tool(
                    ToolCall(
                        id="persona_auto_get_task_status",
                        name="get_task_status",
                        arguments={},
                    ),
                    session_config,
                )
                auto_tool_results["get_task_status"] = tool_result.result
                return self._auto_task_status_instructions(tool_result.result), False
            return None, False

        async def routed_input_stream() -> AsyncIterator[VoiceLLMInputEvent]:
            async for input_event in input_stream:
                if input_event.tool_result or input_event.response_instructions is not None:
                    yield input_event
                    continue
                user_text = self._clean_text(input_event.text)
                if not user_text:
                    yield input_event
                    continue
                try:
                    response_instructions, _clear_turn = await auto_route_user_text(user_text)
                except Exception:
                    logger.exception("persona typed-input pre-response routing failed")
                    response_instructions = None
                if response_instructions is None:
                    yield input_event
                    continue
                if input_event.audio or input_event.image is not None:
                    yield replace(input_event, text="")
                    yield VoiceLLMInputEvent(text=user_text, response_instructions=response_instructions)
                    continue
                yield replace(input_event, response_instructions=response_instructions)

        model_event_task: asyncio.Task[VoiceLLMOutputEvent] | None = None
        task_event_task: asyncio.Task[dict[str, Any]] | None = None
        try:
            model_events = model_plugin.converse_stream(
                self._merged_input_stream(
                    routed_input_stream(),
                    injected,
                    lambda: bool(pending_task_starts or background_tasks),
                ),
                session_config=model_session_config,
            ).__aiter__()
            model_event_task = asyncio.create_task(model_events.__anext__())
            task_event_task = asyncio.create_task(task_events.get())

            while model_event_task is not None:
                wait_set = {model_event_task}
                if task_event_task is not None:
                    wait_set.add(task_event_task)
                done, _pending = await asyncio.wait(wait_set, return_when=asyncio.FIRST_COMPLETED)

                if task_event_task is not None and task_event_task in done:
                    yield VoiceLLMOutputEvent(task_event=task_event_task.result())
                    for payload in self._drain_task_events(task_events):
                        yield VoiceLLMOutputEvent(task_event=payload)
                    task_event_task = asyncio.create_task(task_events.get())
                    if model_event_task not in done:
                        continue

                if model_event_task not in done:
                    continue

                try:
                    event = model_event_task.result()
                except StopAsyncIteration:
                    model_event_task = None
                    break

                self._log_model_event(session_config.session_id, event)
                if event.user_transcript:
                    user_text = event.user_transcript
                    turn_transcripts.append(user_text)
                    yield VoiceLLMOutputEvent(
                        user_transcript=user_text,
                        question_id=event.question_id,
                        reply_id=event.reply_id,
                    )
                    try:
                        response_instructions, clear_turn = await auto_route_user_text(user_text)
                        if response_instructions is not None:
                            for payload in self._drain_task_events(task_events):
                                yield VoiceLLMOutputEvent(task_event=payload)
                            if clear_turn:
                                turn_transcripts.clear()
                        else:
                            response_instructions = await self._rag_response_instructions(user_text, session_config)
                    except Exception:
                        logger.exception("persona pre-response routing failed")
                        response_instructions = ""
                    await injected.put(VoiceLLMInputEvent(response_instructions=response_instructions))
                    event = replace(event, user_transcript="")
                    if not event.tool_calls and not event.barge_in and not self._has_assistant_output(event):
                        model_event_task = asyncio.create_task(model_events.__anext__())
                        continue

                if event.tool_calls:
                    for call in event.tool_calls:
                        name = call.name.strip()
                        final_user_text = self._final_user_text(call, turn_transcripts)
                        effective_call = call
                        if name == "create_task" and final_user_text:
                            args = dict(call.arguments or {})
                            args["description"] = final_user_text
                            effective_call = ToolCall(id=call.id, name=call.name, arguments=args)
                        turn_transcripts.clear()

                        try:
                            if name in auto_tool_results:
                                result = auto_tool_results[name]
                            else:
                                tool_result = await self._execute_tool(effective_call, session_config)
                                if tool_result.pending_task is not None:
                                    pending_task_starts.append(tool_result.pending_task)
                                result = tool_result.result
                        except Exception as exc:
                            logger.exception("persona tool call failed: %s", call.name)
                            result = {"ok": False, "error": str(exc)}
                        await injected.put(
                            VoiceLLMInputEvent(
                                tool_result=ToolResult(
                                    id=call.id,
                                    name=call.name,
                                    result=result,
                                )
                            )
                        )
                    for payload in self._drain_task_events(task_events):
                        yield VoiceLLMOutputEvent(task_event=payload)
                    model_event_task = asyncio.create_task(model_events.__anext__())
                    continue

                if self._has_assistant_output(event) and turn_transcripts:
                    turn_transcripts.clear()
                yield event

                if event.is_final and pending_task_starts:
                    starts = pending_task_starts[:]
                    pending_task_starts.clear()
                    for pending in starts:
                        schedule_task_start(pending)
                if event.is_final:
                    auto_tool_results.clear()
                model_event_task = asyncio.create_task(model_events.__anext__())
            for payload in self._drain_task_events(task_events):
                yield VoiceLLMOutputEvent(task_event=payload)
        finally:
            for task in (model_event_task, task_event_task):
                if task is not None and not task.done():
                    task.cancel()
            await asyncio.gather(
                *(task for task in (model_event_task, task_event_task) if task is not None),
                return_exceptions=True,
            )
            if remove_task_event_listener is not None:
                remove_task_event_listener()
            for task in background_tasks:
                task.cancel()
            if background_tasks:
                await asyncio.gather(*background_tasks, return_exceptions=True)
