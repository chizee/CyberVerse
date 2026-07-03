import asyncio
import json
import logging

import pytest

from inference.core.types import (
    AudioChunk,
    PluginConfig,
    ToolCall,
    VoiceLLMInputEvent,
    VoiceLLMOutputEvent,
    VoiceLLMSessionConfig,
)
from inference.plugins.voice_llm.base import VoiceLLMPlugin
from inference.plugins.voice_llm.persona import runtime as runtime_module
from inference.plugins.voice_llm.persona.runtime import LocalTaskRuntime
from inference.plugins.voice_llm.persona.schemas import ArtifactRequest, Task, TaskEvent
from inference.plugins.voice_llm.persona.subagents.runner import (
    PiSdkSubAgentRunner,
    RoleSubAgentContext,
    RoleSubAgentContextResolver,
)
from inference.plugins.voice_llm.persona_agent import PERSONA_AGENT_INSTRUCTIONS, PersonaAgentPlugin


class FakeOmniPlugin(VoiceLLMPlugin):
    name = "omni.fake"

    async def initialize(self, config):
        self.plugin_name = config.plugin_name
        self.scenario = config.params.get("scenario", "chat")

    async def shutdown(self):
        pass

    async def check_voice(self, session_config=None):
        pass

    async def _next_tool_result(self, input_stream):
        async for event in input_stream:
            if event.tool_result:
                return event.tool_result
        raise AssertionError("expected tool result")

    async def _next_text(self, input_stream):
        async for event in input_stream:
            if event.text:
                return event.text
        raise AssertionError("expected injected text")

    async def _next_response_instructions(self, input_stream):
        async for event in input_stream:
            if event.response_instructions is not None:
                return event.response_instructions
        raise AssertionError("expected response instructions")

    async def _next_input(self, input_stream):
        async for event in input_stream:
            return event
        raise AssertionError("expected input event")

    async def converse_stream(self, input_stream, session_config=None):
        self.last_session_config = session_config
        first_input = None
        async for event in input_stream:
            first_input = event
            break

        if self.scenario == "chat":
            yield VoiceLLMOutputEvent(user_transcript="你好")
            yield VoiceLLMOutputEvent(
                transcript="你好，我在。",
                audio=AudioChunk(data=b"audio", sample_rate=16000, is_final=True),
                is_final=True,
            )
            return

        if self.scenario == "typed_auto_create_task_no_tool":
            assert first_input is not None
            instructions = first_input.response_instructions
            assert instructions is not None
            assert "CyberVerse 已经创建后台任务" in instructions
            assert "不要再次调用 create_task" in instructions
            yield VoiceLLMOutputEvent(
                transcript="好的，后台任务已经开始。",
                audio=AudioChunk(data=b"ack", sample_rate=16000, is_final=True),
                is_final=True,
            )
            final_prompt = await self._next_text(input_stream)
            assert "后台任务结果已经返回" in final_prompt
            yield VoiceLLMOutputEvent(
                transcript="整理好了，资料已经生成。",
                audio=AudioChunk(data=b"done", sample_rate=16000, is_final=True),
                is_final=True,
            )
            return

        if self.scenario == "typed_auto_get_task_status_no_tool":
            assert first_input is not None
            instructions = first_input.response_instructions
            assert instructions is not None
            assert "进度：42%" in instructions
            assert "不要再次调用 get_task_status" in instructions
            yield VoiceLLMOutputEvent(
                transcript="正在检索和整理资料，进度 42%。",
                audio=AudioChunk(data=b"progress", sample_rate=16000, is_final=True),
                is_final=True,
            )
            return

        if self.scenario == "auto_create_task_no_tool":
            yield VoiceLLMOutputEvent(user_transcript="请后台执行一个复杂任务：整理角色独立扩展方案")
            instructions = await self._next_response_instructions(input_stream)
            assert "CyberVerse 已经创建后台任务" in instructions
            assert "不要再次调用 create_task" in instructions
            yield VoiceLLMOutputEvent(
                transcript="好的，后台任务已经开始。",
                audio=AudioChunk(data=b"ack", sample_rate=16000, is_final=True),
                is_final=True,
            )
            final_prompt = await self._next_text(input_stream)
            assert "后台任务结果已经返回" in final_prompt
            yield VoiceLLMOutputEvent(
                transcript="整理好了，资料已经生成。",
                audio=AudioChunk(data=b"done", sample_rate=16000, is_final=True),
                is_final=True,
            )
            return

        if self.scenario == "auto_get_task_status_no_tool":
            yield VoiceLLMOutputEvent(user_transcript="现在执行到哪一步了")
            instructions = await self._next_response_instructions(input_stream)
            assert "进度：42%" in instructions
            assert "不要再次调用 get_task_status" in instructions
            yield VoiceLLMOutputEvent(
                transcript="正在检索和整理资料，进度 42%。",
                audio=AudioChunk(data=b"progress", sample_rate=16000, is_final=True),
                is_final=True,
            )
            return

        tool_name = self.scenario
        transcript_by_tool = {
            "create_task": "今天知乎有哪些热门信息",
            "create_task_search_kind": "查一下今天知乎有什么热门新闻。",
            "legacy_create_task": "今天知乎有哪些热门信息",
            "get_task_status": "现在查得怎么样了",
            "get_task_status_running": "现在执行到哪一步了",
            "cancel_task": "算了不用查了",
        }
        if tool_name in transcript_by_tool:
            yield VoiceLLMOutputEvent(user_transcript=transcript_by_tool[tool_name])
        emitted_tool_name = (
            "create_task"
            if tool_name in {"create_task_search_kind", "legacy_create_task"}
            else "get_task_status"
            if tool_name == "get_task_status_running"
            else tool_name
        )
        if tool_name == "create_task":
            tool_arguments = {"description": "今天知乎有哪些热门信息"}
            expected_request = "今天知乎有哪些热门信息"
        elif tool_name == "create_task_search_kind":
            tool_arguments = {"description": "查询今天知乎上的热门新闻", "kind": "search"}
            expected_request = "查询今天知乎上的热门新闻"
        elif tool_name == "legacy_create_task":
            tool_arguments = {"user_request": "今天知乎有哪些热门信息", "title": "知乎热点", "kind": "search"}
            expected_request = "今天知乎有哪些热门信息"
        elif tool_name == "get_task_status_running":
            tool_arguments = {}
            expected_request = ""
        else:
            tool_arguments = {"user_request": "今天知乎有哪些热门信息", "title": "知乎热点"}
            expected_request = "今天知乎有哪些热门信息"
        yield VoiceLLMOutputEvent(
            tool_calls=[
                ToolCall(
                    id="call-1",
                    name=emitted_tool_name,
                    arguments=tool_arguments,
                )
            ]
        )
        tool_result = await self._next_tool_result(input_stream)
        assert tool_result.name == emitted_tool_name
        if emitted_tool_name == "create_task":
            assert tool_result.result["accepted"] is True
            yield VoiceLLMOutputEvent(
                transcript="好的，请稍等。",
                audio=AudioChunk(data=b"ack", sample_rate=16000, is_final=True),
                is_final=True,
            )
            final_prompt = await self._next_text(input_stream)
            assert f"用户原始请求：{expected_request}" in final_prompt
            assert "任务状态：completed" in final_prompt
            yield VoiceLLMOutputEvent(
                transcript="查好了，资料已经整理好。",
                audio=AudioChunk(data=b"done", sample_rate=16000, is_final=True),
                is_final=True,
            )
            return
        if tool_name == "get_task_status_running":
            result = tool_result.result
            task = result["task"]
            events = result["events"]
            assert task["status"] == "running"
            assert task["progress"] == 42
            assert any(event["event_type"] == "subagent.progress" and event["progress"] == 42 for event in events)
            yield VoiceLLMOutputEvent(
                transcript="正在检索和整理资料，进度 42%。",
                audio=AudioChunk(data=b"progress", sample_rate=16000, is_final=True),
                is_final=True,
            )
            return
        yield VoiceLLMOutputEvent(
            transcript=f"{emitted_tool_name} ok",
            audio=AudioChunk(data=b"ok", sample_rate=16000, is_final=True),
            is_final=True,
        )
        return


class FakeTaskClient:
    def __init__(self):
        self.calls = []

    async def create_task(self, session_id, args):
        self.calls.append(("create_task", session_id, args))
        return {"id": "task-1", "status": "queued"}

    async def get_task(self, task_id):
        self.calls.append(("get_task", task_id, {}))
        return {
            "id": task_id,
            "status": "completed",
            "progress": 100,
            "result_summary": "我已经整理好资料。",
        }

    async def get_task_events(self, task_id, after_seq=0, limit=100):
        self.calls.append(("get_task_events", task_id, {"after_seq": after_seq, "limit": limit}))
        if after_seq > 0:
            return []
        return [
            {
                "task_id": task_id,
                "seq": 1,
                "event_type": "task.completed",
                "status": "completed",
                "message": "我已经整理好资料。",
                "progress": 100,
                "payload": {"artifact_id": "artifact-1"},
            }
        ]

    async def get_task_status(self, session_id):
        self.calls.append(("get_task_status", session_id, {}))
        return {"task": {"id": "task-1", "status": "running", "progress": 30}, "events": []}

    async def cancel_task(self, session_id):
        self.calls.append(("cancel_task", session_id, {}))
        return {"cancelled": True, "task": {"id": "task-1", "status": "cancelled"}}

    async def shutdown(self):
        pass


class FakeSubAgentRunner:
    def __init__(
        self,
        *,
        fail: Exception | None = None,
        create_artifact: bool = True,
        delay_seconds: float = 0.0,
    ):
        self.fail = fail
        self.create_artifact = create_artifact
        self.delay_seconds = delay_seconds
        self.calls = []

    async def run(self, task, context, callbacks):
        self.calls.append((task, context))
        if self.fail is not None:
            raise self.fail
        if self.delay_seconds > 0:
            await asyncio.sleep(self.delay_seconds)
        artifact_id = ""
        if self.create_artifact:
            artifact = await callbacks.artifact(
                task.id,
                ArtifactRequest(
                    title="知乎热点",
                    type="html",
                    mime_type="text/html; charset=utf-8",
                    content="<html><body>已完成整理。</body></html>",
                ),
            )
            artifact_id = str(artifact.get("id") or "")
        await callbacks.event(
            task.id,
            TaskEvent(
                event_type="task.completed",
                status="completed",
                message="已整理好资料。",
                progress=100,
                payload={"artifact_id": artifact_id} if artifact_id else None,
            ),
        )


class ProgressingSubAgentRunner:
    def __init__(self, progress_ready, finish_gate):
        self.progress_ready = progress_ready
        self.finish_gate = finish_gate
        self.calls = []

    async def run(self, task, context, callbacks):
        self.calls.append((task, context))
        await callbacks.event(
            task.id,
            TaskEvent(
                event_type="subagent.progress",
                status="running",
                message="正在检索和整理资料。",
                progress=42,
            ),
        )
        self.progress_ready.set()
        await self.finish_gate.wait()
        await callbacks.event(
            task.id,
            TaskEvent(
                event_type="task.completed",
                status="completed",
                message="已整理好资料。",
                progress=100,
            ),
        )


class CollectingCallbacks:
    def __init__(self):
        self.events = []
        self.artifacts = []

    async def event(self, task_id, event):
        self.events.append((task_id, event))

    async def artifact(self, task_id, artifact):
        artifact_id = f"artifact-{len(self.artifacts) + 1}"
        self.artifacts.append((task_id, artifact_id, artifact))
        return {"id": artifact_id}


class FakeProcessStdin:
    def __init__(self):
        self.data = b""
        self.closed = False

    def write(self, data):
        self.data += data

    async def drain(self):
        pass

    def close(self):
        self.closed = True

    def is_closing(self):
        return self.closed

    async def wait_closed(self):
        pass


class FakeProcessStream:
    def __init__(self, lines=(), data=b""):
        self.lines = [
            line if isinstance(line, bytes) else str(line).encode("utf-8")
            for line in lines
        ]
        self.data = data

    async def readline(self):
        if not self.lines:
            return b""
        line = self.lines.pop(0)
        return line if line.endswith(b"\n") else line + b"\n"

    async def read(self):
        return self.data


class BlockingProcessStream:
    async def readline(self):
        await asyncio.Event().wait()

    async def read(self):
        await asyncio.Event().wait()


class FakePiProcess:
    def __init__(self, stdout_lines=(), stderr=b"", returncode=0):
        self.stdin = FakeProcessStdin()
        self.stdout = FakeProcessStream(stdout_lines)
        self.stderr = FakeProcessStream(data=stderr)
        self.returncode = returncode
        self.killed = False
        self.wait_count = 0

    async def wait(self):
        self.wait_count += 1
        return self.returncode

    async def communicate(self):
        return b"", self.stderr.data

    def kill(self):
        self.killed = True


async def make_persona(scenario):
    plugin = PersonaAgentPlugin()
    await plugin.initialize(
        PluginConfig(
            plugin_name="persona.persona",
            params={
                "task_poll_interval_seconds": 0.1,
                "task_monitor_timeout_seconds": 2,
            },
            shared={
                "omni": {
                    "default": "fake",
                    "fake": {
                        "plugin_class": "tests.unit.test_persona_agent_plugin.FakeOmniPlugin",
                        "scenario": scenario,
                    }
                }
            },
        )
    )
    fake_runtime = FakeTaskClient()
    plugin.task_runtime = fake_runtime
    plugin.supervisor.runtime = fake_runtime
    return plugin


async def make_persona_with_local_runtime(scenario, tmp_path, monkeypatch, fake_runner=None):
    fake_runner = fake_runner or FakeSubAgentRunner()
    monkeypatch.setattr(runtime_module, "PiSdkSubAgentRunner", lambda: fake_runner)

    plugin = PersonaAgentPlugin()
    await plugin.initialize(
        PluginConfig(
            plugin_name="persona.persona",
            params={
                "task_poll_interval_seconds": 0.01,
                "task_monitor_timeout_seconds": 2,
            },
            shared={
                "omni": {
                    "default": "fake",
                    "fake": {
                        "plugin_class": "tests.unit.test_persona_agent_plugin.FakeOmniPlugin",
                        "scenario": scenario,
                    }
                },
                "runtime_config": {
                    "inference": {
                        "persona": {
                            "plugin_class": "inference.plugins.voice_llm.persona_agent.PersonaAgentPlugin",
                            "subagent": {
                                "agent_runtime": "pi",
                                "workspace_root": str(tmp_path / "subagents"),
                                "provider": "qwen",
                                "model": "qwen3.6-plus",
                            }
                        }
                    }
                },
            },
        )
    )
    return plugin, fake_runner


async def one_input():
    yield VoiceLLMInputEvent(audio=b"pcm")


async def text_input(text):
    yield VoiceLLMInputEvent(text=text)


def persona_session(tmp_path):
    return VoiceLLMSessionConfig(
        session_id="session-1",
        character_id="char-1",
        character_dir=str(tmp_path / "characters" / "char-1"),
    )


async def wait_task_terminal(runtime, task_id):
    for _ in range(100):
        task = await runtime.get_task(task_id)
        if task["status"] in {"completed", "failed", "cancelled"}:
            return task
        await asyncio.sleep(0.01)
    raise AssertionError(f"task did not reach terminal status: {task_id}")


def test_persona_agent_auto_task_intent_prefers_explicit_create_over_future_progress():
    assert PersonaAgentPlugin._should_auto_create_task(
        "请后台执行复杂任务：整理 CyberVerse Pi SDK 角色扩展方案。任务开始后我还会继续问你进度。"
    )
    assert not PersonaAgentPlugin._should_auto_create_task("后台任务执行到哪一步了")
    assert PersonaAgentPlugin._should_auto_get_task_status("后台任务执行到哪一步了")


@pytest.mark.asyncio
async def test_persona_agent_passthrough_chat(tmp_path):
    plugin = await make_persona("chat")

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                VoiceLLMSessionConfig(session_id="session-1"),
            )
        ]
        selected_plugin = plugin.model_plugin
    finally:
        await plugin.shutdown()

    assert outputs[0].user_transcript == "你好"
    assert outputs[1].transcript == "你好，我在。"
    assert outputs[1].audio is not None
    tool_names = [tool.name for tool in selected_plugin.last_session_config.tools]
    assert tool_names == ["create_task", "get_task_status", "cancel_task", "retrieve_character_knowledge"]
    assert selected_plugin.last_session_config.defer_response is True
    assert "PersonaAgent" in selected_plugin.last_session_config.system_prompt
    assert "wait_for_more_input" not in selected_plugin.last_session_config.system_prompt
    assert "JSON" not in PERSONA_AGENT_INSTRUCTIONS
    assert plugin.task_runtime.calls == []


@pytest.mark.asyncio
async def test_persona_agent_uses_session_voice_provider(tmp_path):
    plugin = PersonaAgentPlugin()
    await plugin.initialize(
        PluginConfig(
            plugin_name="persona.persona",
            params={},
            shared={
                "omni": {
                    "default": "fake",
                    "fake": {
                        "plugin_class": "tests.unit.test_persona_agent_plugin.FakeOmniPlugin",
                        "scenario": "chat",
                    },
                    "alt": {
                        "plugin_class": "tests.unit.test_persona_agent_plugin.FakeOmniPlugin",
                        "scenario": "chat",
                    },
                }
            },
        )
    )
    fake_runtime = FakeTaskClient()
    plugin.task_runtime = fake_runtime
    plugin.supervisor.runtime = fake_runtime

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                VoiceLLMSessionConfig(session_id="session-1", provider="alt"),
            )
        ]
        selected_plugin = plugin.model_plugins["alt"]
    finally:
        await plugin.shutdown()

    assert outputs[1].transcript == "你好，我在。"
    assert selected_plugin.plugin_name == "omni.alt"
    assert selected_plugin.last_session_config.provider == "alt"


@pytest.mark.asyncio
@pytest.mark.parametrize("tool_name", ["get_task_status", "cancel_task"])
async def test_persona_agent_executes_hidden_tool_calls(tool_name, tmp_path):
    plugin = await make_persona(tool_name)

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                VoiceLLMSessionConfig(session_id="session-1"),
            )
        ]
    finally:
        await plugin.shutdown()

    assert outputs[-1].transcript == f"{tool_name} ok"
    assert outputs[0].user_transcript
    assert plugin.task_runtime.calls[0][0] == tool_name
    assert plugin.task_runtime.calls[0][1] == "session-1"


@pytest.mark.asyncio
async def test_persona_agent_create_task_acks_then_runs_async_task(tmp_path):
    plugin = await make_persona("create_task")
    session_config = persona_session(tmp_path)

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                session_config,
            )
        ]
    finally:
        await plugin.shutdown()

    assert outputs[0].user_transcript == "今天知乎有哪些热门信息"
    assert outputs[1].transcript == "好的，请稍等。"
    assert outputs[-1].transcript == "查好了，资料已经整理好。"
    assert plugin.task_runtime.calls[0] == (
        "create_task",
        "session-1",
        {
            "description": "今天知乎有哪些热门信息",
            "user_request": "今天知乎有哪些热门信息",
            "character_id": "char-1",
            "metadata": {
                "character_dir": session_config.character_dir,
                "source_session_id": "session-1",
            },
        },
    )
    assert any(call[0] == "get_task_events" for call in plugin.task_runtime.calls)


@pytest.mark.asyncio
async def test_persona_agent_ignores_legacy_task_kind(tmp_path):
    plugin = await make_persona("create_task_search_kind")
    session_config = persona_session(tmp_path)

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                session_config,
            )
        ]
    finally:
        await plugin.shutdown()

    assert outputs[0].user_transcript == "查一下今天知乎有什么热门新闻。"
    assert outputs[-1].transcript == "查好了，资料已经整理好。"
    assert plugin.task_runtime.calls[0] == (
        "create_task",
        "session-1",
        {
            "description": "查询今天知乎上的热门新闻",
            "user_request": "查询今天知乎上的热门新闻",
            "character_id": "char-1",
            "metadata": {
                "character_dir": session_config.character_dir,
                "source_session_id": "session-1",
            },
        },
    )


@pytest.mark.asyncio
async def test_persona_agent_accepts_legacy_create_task_args(tmp_path):
    plugin = await make_persona("legacy_create_task")
    session_config = persona_session(tmp_path)

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                session_config,
            )
        ]
    finally:
        await plugin.shutdown()

    assert outputs[-1].transcript == "查好了，资料已经整理好。"
    assert plugin.task_runtime.calls[0] == (
        "create_task",
        "session-1",
        {
            "description": "今天知乎有哪些热门信息",
            "user_request": "今天知乎有哪些热门信息",
            "character_id": "char-1",
            "metadata": {
                "character_dir": session_config.character_dir,
                "source_session_id": "session-1",
            },
        },
    )


@pytest.mark.asyncio
async def test_local_task_runtime_ignores_legacy_kind(tmp_path):
    runtime = LocalTaskRuntime(
        runner=FakeSubAgentRunner(),
        context_resolver=RoleSubAgentContextResolver(
            {
                "inference": {
                    "persona_agent": {
                        "subagent": {
                            "pi": {
                                "workspace_root": str(tmp_path / "pi"),
                            }
                        }
                    }
                }
            }
        ),
    )

    try:
        task = await runtime.create_task(
            "session-1",
            {
                "description": "查询今天知乎上的热门新闻",
                "character_id": "char-1",
                "kind": "search",
            },
        )
    finally:
        await runtime.shutdown()

    assert "kind" not in task
    assert task["user_request"] == "查询今天知乎上的热门新闻"


@pytest.mark.asyncio
async def test_local_task_runtime_fails_without_character_id():
    fake_runner = FakeSubAgentRunner()
    runtime = LocalTaskRuntime(runner=fake_runner)

    try:
        task = await runtime.create_task("session-1", {"description": "整理资料"})
        final_task = await wait_task_terminal(runtime, task["id"])
        events = await runtime.get_task_events(task["id"])
    finally:
        await runtime.shutdown()

    assert final_task["status"] == "failed"
    assert "character_id" in events[-1]["message"]
    assert fake_runner.calls == []


@pytest.mark.asyncio
async def test_local_task_runtime_fails_unknown_role(tmp_path):
    fake_runner = FakeSubAgentRunner()
    runtime = LocalTaskRuntime(
        runner=fake_runner,
        context_resolver=RoleSubAgentContextResolver(
            {
                "inference": {
                    "persona_agent": {
                        "subagent": {
                            "pi": {
                                "workspace_root": str(tmp_path / "pi"),
                                "roles": {"known-role": {}},
                            }
                        }
                    }
                }
            }
        ),
    )

    try:
        task = await runtime.create_task(
            "session-1",
            {"description": "整理资料", "character_id": "unknown-role"},
        )
        final_task = await wait_task_terminal(runtime, task["id"])
        events = await runtime.get_task_events(task["id"])
    finally:
        await runtime.shutdown()

    assert final_task["status"] == "failed"
    assert "unknown-role" in events[-1]["message"]
    assert fake_runner.calls == []


@pytest.mark.asyncio
async def test_local_task_runtime_runner_error_maps_to_failed():
    runtime = LocalTaskRuntime(runner=FakeSubAgentRunner(fail=TimeoutError("pi timed out")))

    try:
        task = await runtime.create_task(
            "session-1",
            {"description": "整理资料", "character_id": "char-1"},
        )
        final_task = await wait_task_terminal(runtime, task["id"])
        events = await runtime.get_task_events(task["id"])
    finally:
        await runtime.shutdown()

    assert final_task["status"] == "failed"
    assert "pi timed out" in events[-1]["message"]


@pytest.mark.asyncio
async def test_local_task_runtime_reports_running_subagent_progress(tmp_path):
    progress_ready = asyncio.Event()
    finish_gate = asyncio.Event()
    fake_runner = ProgressingSubAgentRunner(progress_ready, finish_gate)
    runtime = LocalTaskRuntime(
        runner=fake_runner,
        context_resolver=RoleSubAgentContextResolver(
            {
                "inference": {
                    "persona_agent": {
                        "subagent": {
                            "pi": {
                                "workspace_root": str(tmp_path / "pi"),
                            }
                        }
                    }
                }
            }
        ),
    )

    try:
        task = await runtime.create_task(
            "session-1",
            {"description": "整理复杂资料", "character_id": "char-1"},
        )
        await asyncio.wait_for(progress_ready.wait(), timeout=1)

        status = await runtime.get_task_status("session-1")
        events = status["events"]
        progress_event = next(event for event in events if event["event_type"] == "subagent.progress")

        assert status["task"]["id"] == task["id"]
        assert status["task"]["status"] == "running"
        assert status["task"]["progress"] == 42
        assert progress_event["message"] == "正在检索和整理资料。"
        assert progress_event["progress"] == 42
        assert [event["event_type"] for event in events] == [
            "task.queued",
            "task.started",
            "subagent.progress",
        ]

        finish_gate.set()
        final_task = await wait_task_terminal(runtime, task["id"])
    finally:
        finish_gate.set()
        await runtime.shutdown()

    assert final_task["status"] == "completed"
    assert fake_runner.calls[0][0].character_id == "char-1"


def test_role_context_resolver_isolates_roles(tmp_path):
    resolver = RoleSubAgentContextResolver(
        {
            "inference": {
                "persona_agent": {
                    "subagent": {
                        "pi": {
                            "workspace_root": str(tmp_path / "pi"),
                            "roles": {
                                "char-a": {
                                    "allowed_packages": ["pkg-a"],
                                    "allowed_skills": ["skill-a"],
                                },
                                "char-b": {
                                    "allowed_packages": ["pkg-b"],
                                    "allowed_skills": ["skill-b"],
                                },
                            },
                        }
                    }
                }
            }
        }
    )
    task_a = Task(id="task-a", session_id="session-a", title="A", user_request="整理 A", character_id="char-a")
    task_b = Task(id="task-b", session_id="session-b", title="B", user_request="整理 B", character_id="char-b")

    context_a = resolver.resolve(task_a)
    context_b = resolver.resolve(task_b)

    assert context_a.workspace != context_b.workspace
    assert context_a.agent_dir != context_b.agent_dir
    assert context_a.allowed_packages == ("pkg-a",)
    assert context_b.allowed_packages == ("pkg-b",)
    assert context_a.allowed_skills == ("skill-a",)
    assert context_b.allowed_skills == ("skill-b",)
    assert context_a.agent_dir.endswith("char-a")
    assert context_b.agent_dir.endswith("char-b")


def test_role_context_resolver_respects_explicit_role_agent_dir(tmp_path):
    explicit_agent_dir = tmp_path / "custom-agent"
    resolver = RoleSubAgentContextResolver(
        {
            "inference": {
                "persona_agent": {
                    "subagent": {
                        "pi": {
                            "agent_dir": str(tmp_path / "pi-agent"),
                            "roles": {
                                "char-a": {
                                    "agent_dir": str(explicit_agent_dir),
                                }
                            },
                        }
                    }
                }
            }
        }
    )
    task = Task(id="task-a", session_id="session-a", title="A", user_request="整理 A", character_id="char-a")

    context = resolver.resolve(task)

    assert context.agent_dir == str(explicit_agent_dir)


def test_role_context_resolver_loads_sdk_provider_config(tmp_path):
    resolver = RoleSubAgentContextResolver(
        {
            "inference": {
                "persona_agent": {
                    "subagent": {
                        "pi": {
                            "sdk": {
                                "workspace_root": str(tmp_path / "pi"),
                                "provider": "qwen",
                                "model": "qwen3.6-plus",
                                "provider_api": "openai-completions",
                                "provider_base_url": "${DASHSCOPE_BASE_URL}",
                                "provider_api_key_env": "DASHSCOPE_API_KEY",
                                "roles": {
                                    "char-a": {
                                        "model": "qwen-plus",
                                    }
                                },
                            }
                        }
                    }
                }
            }
        }
    )
    task = Task(id="task-a", session_id="session-a", title="A", user_request="整理 A", character_id="char-a")

    context = resolver.resolve(task)

    assert context.provider == "qwen"
    assert context.model == "qwen-plus"
    assert context.provider_api == "openai-completions"
    assert context.provider_base_url == "${DASHSCOPE_BASE_URL}"
    assert context.provider_api_key_env == "DASHSCOPE_API_KEY"


def test_role_context_resolver_loads_single_layer_subagent_config(tmp_path, monkeypatch):
    config_dir = tmp_path / "config"
    settings_dir = config_dir / "subagents"
    settings_dir.mkdir(parents=True)
    (settings_dir / "pi.json").write_text(
        json.dumps(
            {
                "defaultProjectTrust": "never",
                "compaction": {"enabled": False},
                "terminal": {"showImages": False},
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("CYBERVERSE_CONFIG_DIR", str(config_dir))
    resolver = RoleSubAgentContextResolver(
        {
            "inference": {
                "persona": {
                    "plugin_class": "inference.plugins.voice_llm.persona_agent.PersonaAgentPlugin",
                    "subagent": {
                        "agent_runtime": "pi",
                        "workspace_root": str(tmp_path / "data" / "subagents"),
                        "provider": "qwen",
                        "model": "qwen3.6-plus",
                    },
                }
            }
        }
    )
    task = Task(id="task-a", session_id="session-a", title="A", user_request="整理 A", character_id="char-a")

    context = resolver.resolve(task)

    assert context.workspace == tmp_path / "data" / "subagents" / "pi" / "workspaces" / "char-a"
    assert context.session_dir == tmp_path / "data" / "subagents" / "pi" / "sessions" / "char-a"
    assert context.agent_dir == str(tmp_path / "data" / "subagents" / "pi" / "agents" / "char-a")
    assert context.provider == "qwen"
    assert context.model == "qwen3.6-plus"
    assert context.provider_api == "openai-completions"
    assert context.provider_base_url == "${DASHSCOPE_BASE_URL}"
    assert context.provider_api_key_env == "DASHSCOPE_API_KEY"
    assert context.settings["compaction"]["enabled"] is False


def test_role_context_resolver_resolves_relative_paths(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    resolver = RoleSubAgentContextResolver(
        {
            "inference": {
                "persona_agent": {
                    "subagent": {
                        "pi": {
                            "workspace_root": "./pi-workspaces",
                            "session_root": "./pi-sessions",
                            "agent_dir": "./pi-agent",
                        }
                    }
                }
            }
        }
    )
    task = Task(id="task-a", session_id="session-a", title="A", user_request="整理 A", character_id="char-a")

    context = resolver.resolve(task)

    assert context.workspace == tmp_path / "pi-workspaces" / "char-a"
    assert context.session_dir == tmp_path / "pi-sessions" / "char-a"
    assert context.agent_dir == str(tmp_path / "pi-agent" / "char-a")


def test_role_context_resolver_loads_character_agent_extensions(tmp_path):
    character_dir = tmp_path / "characters" / "char-a"
    character_dir.mkdir(parents=True)
    (character_dir / "character.json").write_text(
        json.dumps(
            {
                "agent_extensions": [
                    {"name": "Research", "url": "https://pi.dev/packages/%40pi/research", "enabled": True},
                    {"name": "Disabled", "url": "npm:@pi/disabled", "enabled": False},
                    {"name": "Blank", "url": " ", "enabled": True},
                ]
            }
        ),
        encoding="utf-8",
    )
    resolver = RoleSubAgentContextResolver(
        {
            "inference": {
                "persona_agent": {
                    "subagent": {
                        "pi": {
                            "sdk": {
                                "workspace_root": str(tmp_path / "pi"),
                                "extension_package_urls": ["npm:@pi/global"],
                                "roles": {
                                    "char-a": {
                                        "extension_package_urls": ["npm:@pi/role"],
                                    }
                                },
                            }
                        }
                    }
                }
            }
        }
    )
    task = Task(
        id="task-a",
        session_id="session-a",
        title="A",
        user_request="整理 A",
        character_id="char-a",
        metadata={"character_dir": str(character_dir)},
    )

    context = resolver.resolve(task)

    assert context.extension_package_urls == ("npm:@pi/role", "npm:@pi/research")


def test_role_context_resolver_ignores_legacy_command_alias(tmp_path):
    resolver = RoleSubAgentContextResolver(
        {
            "inference": {
                "persona_agent": {
                    "subagent": {
                        "pi": {
                            "workspace_root": str(tmp_path / "pi"),
                            "command": "pi",
                            "roles": {
                                "char-a": {
                                    "command": "pi",
                                }
                            },
                        }
                    }
                }
            }
        }
    )
    task = Task(id="task-a", session_id="session-a", title="A", user_request="整理 A", character_id="char-a")

    context = resolver.resolve(task)

    assert context.bridge_command[0] == "node"
    assert context.bridge_command != ("pi",)


@pytest.mark.asyncio
async def test_pi_sdk_subagent_runner_maps_bridge_events_to_artifact_and_completed_event(tmp_path, monkeypatch):
    captured = {}
    monkeypatch.setenv("SHOULD_NOT_LEAK_TO_PI", "secret")

    async def fake_create_subprocess_exec(*command, **kwargs):
        captured["command"] = command
        captured["kwargs"] = kwargs
        process = FakePiProcess(
            [
                json.dumps({"type": "progress", "message": "Pi SDK 已启动", "progress": 25}, ensure_ascii=False),
                json.dumps(
                    {
                        "type": "artifact",
                        "artifact": {
                            "title": "Pi 报告",
                            "type": "markdown",
                            "mime_type": "text/markdown; charset=utf-8",
                            "content": "# done",
                        },
                    },
                    ensure_ascii=False,
                ),
                json.dumps({"type": "completed", "summary": "Pi 已完成"}, ensure_ascii=False),
            ]
        )
        captured["process"] = process
        return process

    monkeypatch.setattr(asyncio, "create_subprocess_exec", fake_create_subprocess_exec)
    runner = PiSdkSubAgentRunner()
    callbacks = CollectingCallbacks()
    task = Task(id="task-1", session_id="session-1", title="T", user_request="整理资料", character_id="char-1")
    context = RoleSubAgentContext(
        character_id="char-1",
        workspace=tmp_path / "workspace",
        session_dir=tmp_path / "sessions",
        session_id="role-char-1",
        allowed_skills=("skill-a",),
        allowed_tools=("custom_tool",),
        allowed_packages=("npm:@pi/allowed",),
        extension_package_urls=("npm:@pi/research",),
        env={"OPENAI_API_KEY": "fake-key"},
        bridge_command=("node", "/opt/cyberverse/inference/pi_bridge/src/bridge.mjs"),
        agent_dir=str(tmp_path / "agent"),
        provider="qwen",
        model="qwen3.6-plus",
        provider_api="openai-completions",
        provider_base_url="https://dashscope.example/compatible-mode/v1",
        provider_api_key_env="DASHSCOPE_API_KEY",
    )

    await runner.run(task, context, callbacks)

    assert captured["command"] == ("node", "/opt/cyberverse/inference/pi_bridge/src/bridge.mjs")
    assert captured["kwargs"]["cwd"] == str(context.workspace)
    assert captured["kwargs"]["env"]["OPENAI_API_KEY"] == "fake-key"
    assert captured["kwargs"]["env"]["CYBERVERSE_PI_AGENT_DIR"] == str(tmp_path / "agent")
    assert "SHOULD_NOT_LEAK_TO_PI" not in captured["kwargs"]["env"]
    request = json.loads(captured["process"].stdin.data.decode("utf-8"))
    assert request["type"] == "run_task"
    assert request["context"]["allowed_skills"] == ["skill-a"]
    assert request["context"]["allowed_tools"] == ["custom_tool"]
    assert request["context"]["allowed_packages"] == ["npm:@pi/allowed"]
    assert request["context"]["extension_package_urls"] == ["npm:@pi/research"]
    assert request["context"]["provider"] == "qwen"
    assert request["context"]["model"] == "qwen3.6-plus"
    assert request["context"]["provider_api"] == "openai-completions"
    assert request["context"]["provider_base_url"] == "https://dashscope.example/compatible-mode/v1"
    assert request["context"]["provider_api_key_env"] == "DASHSCOPE_API_KEY"
    assert callbacks.artifacts[0][2].title == "Pi 报告"
    assert callbacks.events[0][1].event_type == "subagent.progress"
    assert callbacks.events[0][1].message == "Pi SDK 已启动"
    assert callbacks.events[-1][1].event_type == "task.completed"
    assert callbacks.events[-1][1].message == "Pi 已完成"


@pytest.mark.asyncio
async def test_pi_sdk_subagent_runner_waits_for_failed_bridge_process(tmp_path, monkeypatch):
    captured = {}

    async def fake_create_subprocess_exec(*command, **kwargs):
        process = FakePiProcess(
            [
                json.dumps({"type": "failed", "error": "bridge boom"}, ensure_ascii=False),
            ],
            stderr=b"stack trace should be secondary",
            returncode=1,
        )
        captured["process"] = process
        return process

    monkeypatch.setattr(asyncio, "create_subprocess_exec", fake_create_subprocess_exec)
    runner = PiSdkSubAgentRunner()
    task = Task(id="task-1", session_id="session-1", title="T", user_request="整理资料", character_id="char-1")
    context = RoleSubAgentContext(
        character_id="char-1",
        workspace=tmp_path / "workspace",
        session_dir=tmp_path / "sessions",
        session_id="role-char-1",
        bridge_command=("node", "/opt/cyberverse/inference/pi_bridge/src/bridge.mjs"),
        agent_dir=str(tmp_path / "agent"),
    )

    with pytest.raises(RuntimeError, match="bridge boom"):
        await runner.run(task, context, CollectingCallbacks())

    assert captured["process"].stdin.closed is True
    assert captured["process"].wait_count == 1
    assert captured["process"].killed is False


@pytest.mark.asyncio
async def test_pi_sdk_subagent_runner_sends_cancel_request_on_task_cancel(tmp_path, monkeypatch):
    captured = {}

    async def fake_create_subprocess_exec(*command, **kwargs):
        process = FakePiProcess()
        process.stdout = BlockingProcessStream()
        captured["process"] = process
        return process

    monkeypatch.setattr(asyncio, "create_subprocess_exec", fake_create_subprocess_exec)
    runner = PiSdkSubAgentRunner()
    task = Task(id="task-cancel", session_id="session-1", title="T", user_request="整理资料", character_id="char-1")
    context = RoleSubAgentContext(
        character_id="char-1",
        workspace=tmp_path / "workspace",
        session_dir=tmp_path / "sessions",
        session_id="role-char-1",
        bridge_command=("node", "/opt/cyberverse/inference/pi_bridge/src/bridge.mjs"),
        agent_dir=str(tmp_path / "agent"),
    )

    run = asyncio.create_task(runner.run(task, context, CollectingCallbacks()))
    for _ in range(100):
        process = captured.get("process")
        if process is not None and b'"type": "run_task"' in process.stdin.data:
            break
        await asyncio.sleep(0.01)
    else:
        raise AssertionError("runner did not send run_task request")

    run.cancel()
    with pytest.raises(asyncio.CancelledError):
        await run

    lines = [json.loads(line) for line in captured["process"].stdin.data.decode("utf-8").splitlines()]
    assert lines[0]["type"] == "run_task"
    assert lines[1] == {"id": "task-cancel", "type": "cancel"}
    assert captured["process"].stdin.closed is True


@pytest.mark.asyncio
async def test_pi_sdk_subagent_runner_times_out_and_kills_bridge(tmp_path, monkeypatch):
    captured = {}

    async def fake_create_subprocess_exec(*command, **kwargs):
        process = FakePiProcess()
        process.stdout = BlockingProcessStream()
        captured["process"] = process
        return process

    monkeypatch.setattr(asyncio, "create_subprocess_exec", fake_create_subprocess_exec)
    runner = PiSdkSubAgentRunner()
    task = Task(id="task-timeout", session_id="session-1", title="T", user_request="整理资料", character_id="char-1")
    context = RoleSubAgentContext(
        character_id="char-1",
        workspace=tmp_path / "workspace",
        session_dir=tmp_path / "sessions",
        session_id="role-char-1",
        bridge_command=("node", "/opt/cyberverse/inference/pi_bridge/src/bridge.mjs"),
        agent_dir=str(tmp_path / "agent"),
        timeout_seconds=0.01,
    )

    with pytest.raises(TimeoutError, match="timed out"):
        await runner.run(task, context, CollectingCallbacks())

    assert captured["process"].killed is True


@pytest.mark.asyncio
async def test_pi_sdk_subagent_runner_truncates_large_artifact(tmp_path, monkeypatch):
    async def fake_create_subprocess_exec(*command, **kwargs):
        return FakePiProcess(
            [
                json.dumps(
                    {
                        "type": "artifact",
                        "artifact": {
                            "title": "Large",
                            "type": "markdown",
                            "mime_type": "text/markdown; charset=utf-8",
                            "content": "abcdef",
                        },
                    },
                    ensure_ascii=False,
                ),
                json.dumps({"type": "completed", "summary": "done"}, ensure_ascii=False),
            ]
        )

    monkeypatch.setattr(asyncio, "create_subprocess_exec", fake_create_subprocess_exec)
    runner = PiSdkSubAgentRunner()
    callbacks = CollectingCallbacks()
    task = Task(id="task-large", session_id="session-1", title="T", user_request="整理资料", character_id="char-1")
    context = RoleSubAgentContext(
        character_id="char-1",
        workspace=tmp_path / "workspace",
        session_dir=tmp_path / "sessions",
        session_id="role-char-1",
        bridge_command=("node", "/opt/cyberverse/inference/pi_bridge/src/bridge.mjs"),
        agent_dir=str(tmp_path / "agent"),
        artifact_max_bytes=5,
    )

    await runner.run(task, context, callbacks)

    assert callbacks.artifacts[0][2].content == "abcde"


@pytest.mark.asyncio
async def test_persona_agent_projects_local_task_events(tmp_path, monkeypatch):
    plugin, fake_runner = await make_persona_with_local_runtime(
        "create_task",
        tmp_path,
        monkeypatch,
    )
    session_config = persona_session(tmp_path)

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                session_config,
            )
        ]
    finally:
        await plugin.shutdown()

    task_events = [event.task_event for event in outputs if event.task_event]
    event_types = [event["event_type"] for event in task_events]

    assert outputs[0].user_transcript == "今天知乎有哪些热门信息"
    assert outputs[-1].transcript == "查好了，资料已经整理好。"
    assert "task.queued" in event_types
    assert "task.started" in event_types
    assert "artifact.created" in event_types
    assert "task.completed" in event_types
    assert all(event["type"] == "task_event" for event in task_events)
    assert all(event["session_id"] == "session-1" for event in task_events)
    assert any((event.get("payload") or {}).get("artifact_id") for event in task_events)
    assert fake_runner.calls[0][0].character_id == "char-1"
    assert fake_runner.calls[0][1].character_id == "char-1"
    assert str(fake_runner.calls[0][1].workspace).endswith("char-1")
    completed_index = next(
        i
        for i, event in enumerate(outputs)
        if event.task_event and event.task_event["event_type"] == "task.completed"
    )
    final_voice_index = next(
        i for i, event in enumerate(outputs) if event.transcript == "查好了，资料已经整理好。"
    )
    assert completed_index < final_voice_index


@pytest.mark.asyncio
async def test_persona_agent_auto_creates_task_when_model_only_acknowledges(tmp_path, monkeypatch):
    plugin, fake_runner = await make_persona_with_local_runtime(
        "auto_create_task_no_tool",
        tmp_path,
        monkeypatch,
    )
    session_config = persona_session(tmp_path)

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                session_config,
            )
        ]
    finally:
        await plugin.shutdown()

    task_events = [event.task_event for event in outputs if event.task_event]
    event_types = [event["event_type"] for event in task_events]
    transcripts = [event.transcript for event in outputs if event.transcript]

    assert outputs[0].user_transcript == "请后台执行一个复杂任务：整理角色独立扩展方案"
    assert "好的，后台任务已经开始。" in transcripts
    assert transcripts[-1] == "整理好了，资料已经生成。"
    assert "task.queued" in event_types
    assert "task.started" in event_types
    assert "artifact.created" in event_types
    assert "task.completed" in event_types
    assert fake_runner.calls


@pytest.mark.asyncio
async def test_persona_agent_auto_creates_task_from_text_input(tmp_path, monkeypatch):
    plugin, fake_runner = await make_persona_with_local_runtime(
        "typed_auto_create_task_no_tool",
        tmp_path,
        monkeypatch,
    )
    session_config = persona_session(tmp_path)
    user_text = "请后台执行复杂任务：整理 CyberVerse Pi SDK 角色扩展方案。任务开始后我还会继续问你进度。"

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                text_input(user_text),
                session_config,
            )
        ]
    finally:
        await plugin.shutdown()

    task_events = [event.task_event for event in outputs if event.task_event]
    event_types = [event["event_type"] for event in task_events]
    transcripts = [event.transcript for event in outputs if event.transcript]

    assert not [event.user_transcript for event in outputs if event.user_transcript]
    assert "好的，后台任务已经开始。" in transcripts
    assert transcripts[-1] == "整理好了，资料已经生成。"
    assert "task.queued" in event_types
    assert "task.started" in event_types
    assert "artifact.created" in event_types
    assert "task.completed" in event_types
    assert fake_runner.calls
    assert fake_runner.calls[0][0].user_request == user_text


@pytest.mark.asyncio
async def test_persona_agent_waits_for_delayed_task_result_after_input_ends(tmp_path, monkeypatch):
    plugin, fake_runner = await make_persona_with_local_runtime(
        "create_task",
        tmp_path,
        monkeypatch,
        fake_runner=FakeSubAgentRunner(delay_seconds=0.35, create_artifact=False),
    )
    session_config = persona_session(tmp_path)

    try:
        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                session_config,
            )
        ]
    finally:
        await plugin.shutdown()

    assert fake_runner.calls
    transcripts = [event.transcript for event in outputs if event.transcript]
    event_types = [event.task_event["event_type"] for event in outputs if event.task_event]
    assert "好的，请稍等。" in transcripts
    assert transcripts[-1] == "查好了，资料已经整理好。"
    assert "task.completed" in event_types


@pytest.mark.asyncio
async def test_persona_agent_reports_running_task_status_while_subagent_continues(tmp_path, monkeypatch):
    progress_ready = asyncio.Event()
    finish_gate = asyncio.Event()
    fake_runner = ProgressingSubAgentRunner(progress_ready, finish_gate)
    plugin, _ = await make_persona_with_local_runtime(
        "get_task_status_running",
        tmp_path,
        monkeypatch,
        fake_runner=fake_runner,
    )
    session_config = persona_session(tmp_path)

    try:
        task = await plugin.task_runtime.create_task(
            "session-1",
            {
                "description": "整理复杂资料",
                "character_id": "char-1",
                "metadata": {
                    "character_dir": session_config.character_dir,
                    "source_session_id": "session-1",
                },
            },
        )
        await asyncio.wait_for(progress_ready.wait(), timeout=1)

        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                session_config,
            )
        ]
        status_during_progress_answer = await plugin.task_runtime.get_task(task["id"])

        finish_gate.set()
        final_task = await wait_task_terminal(plugin.task_runtime, task["id"])
    finally:
        finish_gate.set()
        await plugin.shutdown()

    assert outputs[0].user_transcript == "现在执行到哪一步了"
    assert outputs[-1].transcript == "正在检索和整理资料，进度 42%。"
    assert status_during_progress_answer["status"] == "running"
    assert status_during_progress_answer["progress"] == 42
    assert final_task["status"] == "completed"
    assert fake_runner.calls


@pytest.mark.asyncio
async def test_persona_agent_auto_reads_task_status_when_model_only_answers_progress(tmp_path, monkeypatch):
    progress_ready = asyncio.Event()
    finish_gate = asyncio.Event()
    fake_runner = ProgressingSubAgentRunner(progress_ready, finish_gate)
    plugin, _ = await make_persona_with_local_runtime(
        "auto_get_task_status_no_tool",
        tmp_path,
        monkeypatch,
        fake_runner=fake_runner,
    )
    session_config = persona_session(tmp_path)

    try:
        task = await plugin.task_runtime.create_task(
            "session-1",
            {
                "description": "整理复杂资料",
                "character_id": "char-1",
                "metadata": {
                    "character_dir": session_config.character_dir,
                    "source_session_id": "session-1",
                },
            },
        )
        await asyncio.wait_for(progress_ready.wait(), timeout=1)

        outputs = [
            event
            async for event in plugin.converse_stream(
                one_input(),
                session_config,
            )
        ]
        status_during_progress_answer = await plugin.task_runtime.get_task(task["id"])

        finish_gate.set()
        final_task = await wait_task_terminal(plugin.task_runtime, task["id"])
    finally:
        finish_gate.set()
        await plugin.shutdown()

    assert outputs[0].user_transcript == "现在执行到哪一步了"
    assert outputs[-1].transcript == "正在检索和整理资料，进度 42%。"
    assert status_during_progress_answer["status"] == "running"
    assert status_during_progress_answer["progress"] == 42
    assert final_task["status"] == "completed"
    assert fake_runner.calls


@pytest.mark.asyncio
async def test_persona_agent_auto_reads_task_status_from_text_input(tmp_path, monkeypatch):
    progress_ready = asyncio.Event()
    finish_gate = asyncio.Event()
    fake_runner = ProgressingSubAgentRunner(progress_ready, finish_gate)
    plugin, _ = await make_persona_with_local_runtime(
        "typed_auto_get_task_status_no_tool",
        tmp_path,
        monkeypatch,
        fake_runner=fake_runner,
    )
    session_config = persona_session(tmp_path)

    try:
        task = await plugin.task_runtime.create_task(
            "session-1",
            {
                "description": "整理复杂资料",
                "character_id": "char-1",
                "metadata": {
                    "character_dir": session_config.character_dir,
                    "source_session_id": "session-1",
                },
            },
        )
        await asyncio.wait_for(progress_ready.wait(), timeout=1)

        outputs = [
            event
            async for event in plugin.converse_stream(
                text_input("现在执行到哪一步了"),
                session_config,
            )
        ]
        status_during_progress_answer = await plugin.task_runtime.get_task(task["id"])

        finish_gate.set()
        final_task = await wait_task_terminal(plugin.task_runtime, task["id"])
    finally:
        finish_gate.set()
        await plugin.shutdown()

    assert not [event.user_transcript for event in outputs if event.user_transcript]
    assert outputs[-1].transcript == "正在检索和整理资料，进度 42%。"
    assert status_during_progress_answer["status"] == "running"
    assert status_during_progress_answer["progress"] == 42
    assert final_task["status"] == "completed"
    assert fake_runner.calls


def test_persona_event_logs_keep_stream_deltas_out_of_info(caplog):
    logger_name = "inference.plugins.voice_llm.persona_agent"

    with caplog.at_level(logging.INFO, logger=logger_name):
        PersonaAgentPlugin._log_model_event(
            "session-1",
            VoiceLLMOutputEvent(transcript="收"),
        )
        PersonaAgentPlugin._log_model_event(
            "session-1",
            VoiceLLMOutputEvent(
                transcript="收到",
                audio=AudioChunk(data=b"", sample_rate=24000, is_final=True),
                is_final=True,
            ),
        )
        PersonaAgentPlugin._log_model_event(
            "session-1",
            VoiceLLMOutputEvent(audio=AudioChunk(data=b"pcm", sample_rate=24000)),
        )

    messages = [record.getMessage() for record in caplog.records]
    assert len(messages) == 1
    assert "kind=assistant_final" in messages[0]
    assert "收到" in messages[0]
