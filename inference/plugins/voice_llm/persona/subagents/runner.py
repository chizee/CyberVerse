from __future__ import annotations

import asyncio
import json
import os
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Protocol
from urllib.parse import unquote, urlparse

from inference.plugins.voice_llm.persona.i18n import Localizer
from inference.plugins.voice_llm.persona.schemas import ArtifactRequest, Task, TaskEvent


class TaskCallbacks(Protocol):
    async def event(self, task_id: str, event: TaskEvent) -> None:
        ...

    async def artifact(self, task_id: str, artifact: ArtifactRequest) -> dict[str, Any]:
        ...


class SubAgentRunner(Protocol):
    async def run(self, task: Task, context: RoleSubAgentContext, callbacks: TaskCallbacks) -> None:
        ...


@dataclass(frozen=True)
class RoleSubAgentContext:
    character_id: str
    workspace: Path
    session_dir: Path
    session_id: str
    character_dir: str = ""
    allowed_packages: tuple[str, ...] = ()
    allowed_skills: tuple[str, ...] = ()
    allowed_tools: tuple[str, ...] = ()
    extension_paths: tuple[str, ...] = ()
    extension_package_urls: tuple[str, ...] = ()
    env: dict[str, str] = field(default_factory=dict)
    timeout_seconds: float = 1800.0
    artifact_max_bytes: int = 1_000_000
    bridge_command: tuple[str, ...] = ()
    bridge_args: tuple[str, ...] = ()
    agent_dir: str = ""
    provider: str = ""
    model: str = ""
    provider_api: str = ""
    provider_base_url: str = ""
    provider_api_key_env: str = ""
    no_builtin_tools: bool = True
    settings: dict[str, Any] = field(default_factory=dict)


_SAFE_ID_RE = re.compile(r"[^A-Za-z0-9_.-]+")
_REPO_ROOT = Path(__file__).resolve().parents[5]
_DEFAULT_BRIDGE_ENTRY = _REPO_ROOT / "inference" / "pi_bridge" / "src" / "bridge.mjs"


def _persona_runtime_params(runtime_config: dict[str, Any] | None) -> dict[str, Any]:
    inference = runtime_config.get("inference", {}) if isinstance(runtime_config, dict) else {}
    inference = inference if isinstance(inference, dict) else {}
    persona_agent = inference.get("persona_agent", {})
    if isinstance(persona_agent, dict) and persona_agent:
        return persona_agent
    persona_section = inference.get("persona", {})
    persona_section = persona_section if isinstance(persona_section, dict) else {}
    if persona_section.get("plugin_class"):
        return persona_section
    persona_plugin = persona_section.get("persona", {})
    return persona_plugin if isinstance(persona_plugin, dict) else {}


def _safe_role_id(value: str) -> str:
    clean = _SAFE_ID_RE.sub("_", value.strip())
    return clean.strip("._-") or "role"


def _string_list(value: Any) -> tuple[str, ...]:
    if isinstance(value, str):
        return (value.strip(),) if value.strip() else ()
    if not isinstance(value, list | tuple | set):
        return ()
    items: list[str] = []
    for item in value:
        text = str(item or "").strip()
        if text:
            items.append(text)
    return tuple(items)


def _normalize_extension_source(value: Any) -> str:
    text = str(value or "").strip()
    if not text:
        return ""
    parsed = urlparse(text)
    if parsed.scheme in {"http", "https"} and parsed.netloc == "pi.dev" and parsed.path.startswith("/packages/"):
        package_name = unquote(parsed.path.removeprefix("/packages/")).strip("/")
        if package_name:
            return f"npm:{package_name}"
    return text


def _extension_source_list(value: Any) -> tuple[str, ...]:
    return tuple(source for source in (_normalize_extension_source(item) for item in _string_list(value)) if source)


def _command_tuple(value: Any, default: tuple[str, ...]) -> tuple[str, ...]:
    if isinstance(value, str) and value.strip():
        return (value.strip(),)
    parsed = _string_list(value)
    return parsed or default


def _positive_float(value: Any, default: float) -> float:
    try:
        parsed = float(value)
    except (TypeError, ValueError):
        parsed = default
    return max(1.0, parsed)


def _positive_int(value: Any, default: int) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        parsed = default
    return max(1, parsed)


def _bool_value(value: Any, default: bool) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    text = str(value).strip().lower()
    if text in {"1", "true", "yes", "on"}:
        return True
    if text in {"0", "false", "no", "off"}:
        return False
    return default


def _dict_value(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _path_value(value: Any) -> Path:
    return Path(str(value)).expanduser().resolve()


def _deep_merge(base: dict[str, Any], override: dict[str, Any]) -> dict[str, Any]:
    merged = dict(base)
    for key, value in override.items():
        if isinstance(value, dict) and isinstance(merged.get(key), dict):
            merged[key] = _deep_merge(merged[key], value)
        else:
            merged[key] = value
    return merged


def _load_json_mapping(path: Path) -> dict[str, Any]:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return {}
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid Pi subagent settings JSON: {path}") from exc
    if not isinstance(data, dict):
        raise ValueError(f"Pi subagent settings must be a JSON object: {path}")
    return data


def _load_pi_settings(config_dir: Path) -> dict[str, Any]:
    path = (config_dir / "subagents" / "pi.json").resolve()
    if path.exists():
        return _load_json_mapping(path)
    return {}


def _provider_defaults(provider: str) -> dict[str, str]:
    if provider == "qwen":
        return {
            "provider_api": "openai-completions",
            "provider_base_url": "${DASHSCOPE_BASE_URL}",
            "provider_api_key_env": "DASHSCOPE_API_KEY",
        }
    return {}


def _character_extensions(character_dir: str) -> tuple[str, ...]:
    if not character_dir:
        return ()
    path = Path(character_dir).expanduser() / "character.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return ()
    raw_extensions = data.get("agent_extensions")
    if not isinstance(raw_extensions, list):
        return ()
    urls: list[str] = []
    for item in raw_extensions:
        if not isinstance(item, dict):
            continue
        if item.get("enabled") is False:
            continue
        url = _normalize_extension_source(item.get("url") or item.get("package_url") or item.get("source"))
        if url:
            urls.append(url)
    return tuple(urls)


class RoleSubAgentContextResolver:
    def __init__(self, runtime_config: dict[str, Any] | None = None) -> None:
        persona_params = _persona_runtime_params(runtime_config)
        subagent_config = _dict_value(persona_params.get("subagent") or persona_params.get("sub_agent"))
        agent_runtime = str(subagent_config.get("agent_runtime") or subagent_config.get("runtime") or "pi").strip()
        if agent_runtime != "pi":
            raise ValueError(f"unsupported subagent agent_runtime: {agent_runtime}")
        pi_root_config = _dict_value(subagent_config.get("pi"))
        pi_config = _dict_value(pi_root_config.get("sdk"))
        if not pi_config:
            pi_config = pi_root_config
        if not pi_config:
            pi_config = _dict_value(persona_params.get("pi"))
        legacy_pi_layout = bool(pi_config)
        if not pi_config:
            pi_config = {
                key: value
                for key, value in subagent_config.items()
                if key not in {"agent_runtime", "runtime", "pi"}
            }

        config_dir = _path_value(os.getenv("CYBERVERSE_CONFIG_DIR", "."))
        workspace_root = pi_config.get("workspace_root") or subagent_config.get("workspace_root")
        if legacy_pi_layout:
            self.workspace_root = _path_value(workspace_root) if workspace_root else config_dir / "data" / "subagents" / "pi"
        else:
            base_root = _path_value(workspace_root) if workspace_root else Path("data/subagents").resolve()
            runtime_root = base_root / agent_runtime
            self.workspace_root = runtime_root / "workspaces"
        session_root = pi_config.get("session_root") or subagent_config.get("session_root")
        if session_root:
            self.session_root = _path_value(session_root)
        elif legacy_pi_layout:
            self.session_root = None
        else:
            self.session_root = runtime_root / "sessions"
        agent_dir = pi_config.get("agent_dir") or subagent_config.get("agent_dir")
        if agent_dir:
            self.agent_dir = _path_value(agent_dir)
        elif legacy_pi_layout:
            self.agent_dir = config_dir / "data" / "subagents" / "pi_agent"
        else:
            self.agent_dir = runtime_root / "agents"
        inline_settings = _dict_value(pi_config.get("settings"))
        self.default_settings = _deep_merge(
            _load_pi_settings(config_dir),
            inline_settings,
        )

        self.default_timeout_seconds = _positive_float(pi_config.get("timeout_seconds"), 1800.0)
        self.default_artifact_max_bytes = _positive_int(pi_config.get("artifact_max_bytes"), 1_000_000)
        self.default_allowed_packages = _string_list(pi_config.get("allowed_packages") or pi_config.get("packages"))
        self.default_allowed_skills = _string_list(pi_config.get("allowed_skills") or pi_config.get("skills"))
        self.default_allowed_tools = _string_list(pi_config.get("allowed_tools") or pi_config.get("tools"))
        self.default_extension_paths = _string_list(pi_config.get("extension_paths") or pi_config.get("extensions"))
        self.default_extension_package_urls = _extension_source_list(
            pi_config.get("extension_package_urls") or pi_config.get("extension_urls") or pi_config.get("package_urls")
        )
        default_bridge = ("node", str(_DEFAULT_BRIDGE_ENTRY))
        self.default_command = _command_tuple(pi_config.get("bridge_command"), default_bridge)
        self.default_args = _string_list(pi_config.get("args"))
        self.default_provider = str(pi_config.get("provider") or "").strip()
        self.default_model = str(pi_config.get("model") or "").strip()
        self.default_provider_api = str(pi_config.get("provider_api") or pi_config.get("api") or "").strip()
        self.default_provider_base_url = str(
            pi_config.get("provider_base_url") or pi_config.get("base_url") or ""
        ).strip()
        self.default_provider_api_key_env = str(
            pi_config.get("provider_api_key_env") or pi_config.get("api_key_env") or ""
        ).strip()
        self.default_no_builtin_tools = _bool_value(pi_config.get("no_builtin_tools"), True)
        self.roles = _dict_value(pi_config.get("roles") or subagent_config.get("roles"))
        self.require_known_roles = _bool_value(pi_config.get("require_known_roles"), bool(self.roles))

    def resolve(self, task: Task) -> RoleSubAgentContext:
        character_id = str(task.character_id or "").strip()
        if not character_id:
            raise ValueError("subagent task requires character_id for role isolation")

        role_config = self.roles.get(character_id)
        if role_config is None and self.require_known_roles:
            raise ValueError(f"unknown subagent role: {character_id}")
        role_config = _dict_value(role_config)
        safe_id = _safe_role_id(character_id)

        workspace = role_config.get("workspace") or role_config.get("pi_workspace")
        workspace_path = _path_value(workspace) if workspace else self.workspace_root / safe_id
        role_agent_dir_value = role_config.get("agent_dir")
        agent_dir = (
            _path_value(role_agent_dir_value)
            if role_agent_dir_value
            else self.agent_dir / safe_id
        )
        session_dir_value = role_config.get("session_dir")
        if session_dir_value:
            session_dir = _path_value(session_dir_value)
        elif self.session_root is not None:
            session_dir = self.session_root / safe_id
        else:
            session_dir = workspace_path / "sessions"

        metadata = task.metadata if isinstance(task.metadata, dict) else {}
        character_dir = str(role_config.get("character_dir") or metadata.get("character_dir") or "").strip()
        session_id = str(role_config.get("session_id") or f"role-{safe_id}").strip()

        env = self._resolve_env(role_config)
        timeout_seconds = _positive_float(role_config.get("timeout_seconds"), self.default_timeout_seconds)
        artifact_max_bytes = _positive_int(role_config.get("artifact_max_bytes"), self.default_artifact_max_bytes)
        extension_paths = _string_list(role_config.get("extension_paths") or role_config.get("extensions")) or self.default_extension_paths
        configured_urls = (
            _extension_source_list(
                role_config.get("extension_package_urls")
                or role_config.get("extension_urls")
                or role_config.get("package_urls")
            )
            or self.default_extension_package_urls
        )
        character_urls = _character_extensions(character_dir)
        provider = str(role_config.get("provider") or self.default_provider).strip()
        provider_defaults = _provider_defaults(provider)
        provider_api = str(
            role_config.get("provider_api")
            or role_config.get("api")
            or self.default_provider_api
            or provider_defaults.get("provider_api", "")
        ).strip()
        provider_base_url = str(
            role_config.get("provider_base_url")
            or role_config.get("base_url")
            or self.default_provider_base_url
            or provider_defaults.get("provider_base_url", "")
        ).strip()
        provider_api_key_env = str(
            role_config.get("provider_api_key_env")
            or role_config.get("api_key_env")
            or self.default_provider_api_key_env
            or provider_defaults.get("provider_api_key_env", "")
        ).strip()
        settings = _deep_merge(self.default_settings, _dict_value(role_config.get("settings")))

        return RoleSubAgentContext(
            character_id=character_id,
            character_dir=character_dir,
            workspace=workspace_path,
            session_dir=session_dir,
            session_id=session_id,
            allowed_packages=_string_list(role_config.get("allowed_packages") or role_config.get("packages")) or self.default_allowed_packages,
            allowed_skills=_string_list(role_config.get("allowed_skills") or role_config.get("skills")) or self.default_allowed_skills,
            allowed_tools=_string_list(role_config.get("allowed_tools") or role_config.get("tools")) or self.default_allowed_tools,
            extension_paths=extension_paths,
            extension_package_urls=tuple(dict.fromkeys((*configured_urls, *character_urls))),
            env=env,
            timeout_seconds=timeout_seconds,
            artifact_max_bytes=artifact_max_bytes,
            bridge_command=_command_tuple(role_config.get("bridge_command"), self.default_command),
            bridge_args=_string_list(role_config.get("args")) or self.default_args,
            agent_dir=str(agent_dir),
            provider=provider,
            model=str(role_config.get("model") or self.default_model).strip(),
            provider_api=provider_api,
            provider_base_url=provider_base_url,
            provider_api_key_env=provider_api_key_env,
            no_builtin_tools=_bool_value(role_config.get("no_builtin_tools"), self.default_no_builtin_tools),
            settings=settings,
        )

    def _resolve_env(self, role_config: dict[str, Any]) -> dict[str, str]:
        env = dict(os.environ)
        inline_env = role_config.get("env_values")
        if isinstance(inline_env, dict):
            for key, value in inline_env.items():
                name = str(key or "").strip()
                if name:
                    env[name] = str(value or "")
        return env


class PiSdkSubAgentRunner:
    async def run(self, task: Task, context: RoleSubAgentContext, callbacks: TaskCallbacks) -> None:
        context.workspace.mkdir(parents=True, exist_ok=True)
        context.session_dir.mkdir(parents=True, exist_ok=True)
        if context.agent_dir:
            Path(context.agent_dir).mkdir(parents=True, exist_ok=True)
        request = self._build_request(task, context)
        command = self._build_command(context)
        env = {
            key: os.environ[key]
            for key in ("PATH", "HOME", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "SHELL")
            if key in os.environ
        }
        env.update(context.env)
        env["CYBERVERSE_PI_AGENT_DIR"] = str(context.agent_dir)
        env["CYBERVERSE_PI_SESSION_DIR"] = str(context.session_dir)

        process = await asyncio.create_subprocess_exec(
            *command,
            cwd=str(context.workspace),
            env=env,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        try:
            await self._run_bridge_process(process, request, task, context, callbacks)
        except asyncio.TimeoutError:
            process.kill()
            await process.communicate()
            raise TimeoutError(f"Pi SDK subagent timed out after {context.timeout_seconds:.0f}s") from None

    async def _run_bridge_process(
        self,
        process: asyncio.subprocess.Process,
        request: dict[str, Any],
        task: Task,
        context: RoleSubAgentContext,
        callbacks: TaskCallbacks,
    ) -> None:
        assert process.stdin is not None
        assert process.stdout is not None
        process.stdin.write((json.dumps(request, ensure_ascii=False) + "\n").encode("utf-8"))
        await process.stdin.drain()
        request_id = str(request.get("id") or task.id)

        stdout_task = asyncio.create_task(self._consume_stdout(process.stdout, task, context, callbacks))
        stderr_task = asyncio.create_task(process.stderr.read() if process.stderr is not None else self._empty_bytes())
        try:
            events = await asyncio.wait_for(stdout_task, timeout=context.timeout_seconds)
            await self._close_stdin(process)
            returncode = await process.wait()
            stderr = await stderr_task
        except asyncio.CancelledError:
            await self._request_cancel(process, request_id)
            await self._terminate_process(process, timeout=5.0)
            raise
        finally:
            for pending in (stdout_task, stderr_task):
                if not pending.done():
                    pending.cancel()

        completed = False
        failed_message = ""
        for event in events:
            if event[0] == "completed":
                completed = True
            if event[0] == "failed" and event[1] and not failed_message:
                failed_message = event[1]
        if returncode != 0:
            detail = stderr.decode("utf-8", errors="replace").strip() if isinstance(stderr, bytes) else ""
            raise RuntimeError(f"Pi SDK bridge failed: {failed_message or detail or f'exit code {returncode}'}")
        if failed_message:
            raise RuntimeError(f"Pi SDK bridge failed: {failed_message}")
        if not completed:
            raise RuntimeError("Pi SDK bridge exited without a completed event")

    async def _empty_bytes(self) -> bytes:
        return b""

    async def _consume_stdout(
        self,
        stream: asyncio.StreamReader,
        task: Task,
        context: RoleSubAgentContext,
        callbacks: TaskCallbacks,
    ) -> list[tuple[str, str]]:
        seen: list[tuple[str, str]] = []
        while True:
            line = await stream.readline()
            if not line:
                return seen
            try:
                event = json.loads(line.decode("utf-8", errors="replace"))
            except json.JSONDecodeError:
                continue
            if not isinstance(event, dict):
                continue
            kind = str(event.get("type") or "").strip()
            if kind == "progress":
                await callbacks.event(
                    task.id,
                    TaskEvent(
                        event_type=str(event.get("event_type") or "subagent.progress"),
                        status="running",
                        message=str(event.get("message") or "SubAgent 正在执行任务。").strip(),
                        progress=_positive_progress(event.get("progress"), 30),
                        payload=_payload_dict(event.get("payload")),
                    ),
                )
            elif kind == "artifact":
                artifact = event.get("artifact")
                if not isinstance(artifact, dict):
                    artifact = event
                created = await callbacks.artifact(task.id, self._artifact_request(artifact, context))
                artifact_id = str(created.get("id") or "").strip() if isinstance(created, dict) else ""
                seen.append(("artifact", artifact_id))
            elif kind == "completed":
                summary = str(event.get("summary") or "后台任务已完成。").strip()
                artifact_id = next((item[1] for item in seen if item[0] == "artifact" and item[1]), "")
                await callbacks.event(
                    task.id,
                    TaskEvent(
                        event_type="task.completed",
                        status="completed",
                        message=summary,
                        progress=100,
                        payload={"artifact_id": artifact_id} if artifact_id else None,
                    ),
                )
                seen.append(("completed", ""))
                return seen
            elif kind == "failed":
                seen.append(("failed", str(event.get("error") or "Pi SDK bridge failed").strip()))
                return seen

    async def _request_cancel(self, process: asyncio.subprocess.Process, request_id: str) -> None:
        stdin = process.stdin
        if stdin is None or stdin.is_closing():
            return
        try:
            stdin.write((json.dumps({"id": request_id, "type": "cancel"}, ensure_ascii=False) + "\n").encode("utf-8"))
            await stdin.drain()
        except (BrokenPipeError, ConnectionResetError):
            pass
        finally:
            await self._close_stdin(process)

    async def _close_stdin(self, process: asyncio.subprocess.Process) -> None:
        stdin = process.stdin
        if stdin is None or stdin.is_closing():
            return
        stdin.close()
        wait_closed = getattr(stdin, "wait_closed", None)
        if callable(wait_closed):
            try:
                await wait_closed()
            except (BrokenPipeError, ConnectionResetError):
                pass

    async def _terminate_process(self, process: asyncio.subprocess.Process, *, timeout: float) -> None:
        if process.returncode is not None:
            return
        try:
            await asyncio.wait_for(process.wait(), timeout=timeout)
            return
        except asyncio.TimeoutError:
            process.kill()
            await process.communicate()

    def _build_request(self, task: Task, context: RoleSubAgentContext) -> dict[str, Any]:
        return {
            "id": task.id,
            "type": "run_task",
            "task": {
                "id": task.id,
                "title": task.title,
                "user_request": task.user_request,
                "locale": Localizer(task.locale).locale,
            },
            "context": {
                "character_id": context.character_id,
                "character_dir": context.character_dir,
                "workspace": str(context.workspace),
                "session_dir": str(context.session_dir),
                "session_id": context.session_id,
                "agent_dir": context.agent_dir,
                "allowed_packages": list(context.allowed_packages),
                "allowed_skills": list(context.allowed_skills),
                "allowed_tools": list(context.allowed_tools),
                "extension_paths": list(context.extension_paths),
                "extension_package_urls": list(context.extension_package_urls),
                "provider": context.provider,
                "model": context.model,
                "provider_api": context.provider_api,
                "provider_base_url": context.provider_base_url,
                "provider_api_key_env": context.provider_api_key_env,
                "no_builtin_tools": context.no_builtin_tools,
                "artifact_max_bytes": context.artifact_max_bytes,
                "settings": context.settings,
            },
            "prompt": self._build_prompt(task, context),
        }

    def _build_command(self, context: RoleSubAgentContext) -> list[str]:
        return [*context.bridge_command, *context.bridge_args]

    def _build_prompt(self, task: Task, context: RoleSubAgentContext) -> str:
        localizer = Localizer(task.locale)
        payload = {
            "task_id": task.id,
            "title": task.title,
            "user_request": task.user_request,
            "locale": localizer.locale,
            "character_id": context.character_id,
            "character_dir": context.character_dir,
            "allowed_packages": list(context.allowed_packages),
            "allowed_skills": list(context.allowed_skills),
            "extension_package_urls": list(context.extension_package_urls),
        }
        return "\n".join(
            [
                "你是 CyberVerse 当前角色的后台 SubAgent。",
                "请在当前 Pi 角色 workspace 中完成用户任务。",
                "你可以使用 CyberVerse 提供的进度和 artifact 工具汇报过程与结果。",
                "最终回复应总结完成情况；如果生成了可交付资料，请调用 artifact 工具或输出 artifacts JSON。",
                json.dumps(payload, ensure_ascii=False),
            ]
        )

    def _artifact_request(self, artifact: dict[str, Any], context: RoleSubAgentContext) -> ArtifactRequest:
        content = str(artifact.get("content") or "")
        encoded = content.encode("utf-8")
        if len(encoded) > context.artifact_max_bytes:
            content = encoded[: context.artifact_max_bytes].decode("utf-8", errors="ignore")
        title = str(artifact.get("title") or "SubAgent 产物").strip()
        artifact_type = str(artifact.get("type") or "markdown").strip()
        mime_type = str(artifact.get("mime_type") or "text/markdown; charset=utf-8").strip()
        return ArtifactRequest(
            title=title,
            content=content,
            type=artifact_type,
            mime_type=mime_type,
            metadata={
                "runner": "pi_sdk",
                "character_id": context.character_id,
                "workspace": str(context.workspace),
                "extension_package_urls": list(context.extension_package_urls),
            },
        )


def _positive_progress(value: Any, default: int) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        parsed = default
    return min(99, max(1, parsed))


def _payload_dict(value: Any) -> dict[str, Any] | None:
    return value if isinstance(value, dict) else None


# Backward-compatible name for existing imports. This no longer calls the pi CLI.
PiSubAgentRunner = PiSdkSubAgentRunner
