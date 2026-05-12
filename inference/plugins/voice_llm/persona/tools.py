from __future__ import annotations

import os
import re
import time
from dataclasses import dataclass
from typing import Any, Protocol


_ENV_PLACEHOLDER_RE = re.compile(r"^\$\{[A-Za-z_][A-Za-z0-9_]*\}$")


def _clean_config_string(value: Any) -> str:
    text = str(value or "").strip()
    if _ENV_PLACEHOLDER_RE.match(text):
        return ""
    return text


def _optional_float(value: Any, default: float) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def _bounded_int(value: Any, default: int, minimum: int, maximum: int) -> int:
    try:
        number = int(value)
    except (TypeError, ValueError):
        number = default
    if number < minimum:
        return minimum
    if number > maximum:
        return maximum
    return number


def _clip_text(value: Any, limit: int = 1600) -> str:
    text = str(value or "").strip()
    if len(text) <= limit:
        return text
    return text[:limit] + "..."


@dataclass(frozen=True)
class SearchResult:
    title: str
    url: str
    snippet: str


class SearchTool(Protocol):
    async def search(self, query: str, limit: int = 5) -> list[SearchResult]:
        ...


class NullSearchTool:
    async def search(self, query: str, limit: int = 5) -> list[SearchResult]:
        return []


class MockSearchTool:
    def __init__(self, results: list[SearchResult] | None = None) -> None:
        self.results = results or [
            SearchResult(
                title="Mock result",
                url="https://example.com/mock",
                snippet="Mock search result for PersonaAgent task tests.",
            )
        ]

    async def search(self, query: str, limit: int = 5) -> list[SearchResult]:
        return self.results[:limit]


@dataclass
class ZhihuConfig:
    access_secret: str = ""
    api_base: str = "https://developer.zhihu.com"
    timeout_seconds: float = 30.0
    zhida_model: str = "zhida-fast-1p5"


def zhihu_config_from_runtime_config(config: dict[str, Any] | None = None) -> ZhihuConfig:
    inference = config.get("inference", {}) if isinstance(config, dict) else {}
    inference = inference if isinstance(inference, dict) else {}
    persona_agent = inference.get("persona_agent", {})
    persona_agent = persona_agent if isinstance(persona_agent, dict) else {}
    persona_section = inference.get("persona", {})
    persona_section = persona_section if isinstance(persona_section, dict) else {}
    persona_plugin = persona_section.get("persona", {})
    persona_plugin = persona_plugin if isinstance(persona_plugin, dict) else {}

    persona_agent_tools = persona_agent.get("tools", {})
    persona_agent_tools = persona_agent_tools if isinstance(persona_agent_tools, dict) else {}
    persona_plugin_tools = persona_plugin.get("tools", {})
    persona_plugin_tools = persona_plugin_tools if isinstance(persona_plugin_tools, dict) else {}

    zhihu = persona_agent_tools.get("zhihu")
    if not isinstance(zhihu, dict):
        zhihu = persona_plugin_tools.get("zhihu")
    if not isinstance(zhihu, dict):
        zhihu = persona_agent.get("zhihu")
    if not isinstance(zhihu, dict):
        zhihu = persona_plugin.get("zhihu")
    zhihu = zhihu if isinstance(zhihu, dict) else {}

    access_secret = _clean_config_string(zhihu.get("access_secret")) or _clean_config_string(
        os.getenv("ZHIHU_ACCESS_SECRET")
    )
    api_base = (
        _clean_config_string(zhihu.get("api_base"))
        or _clean_config_string(os.getenv("ZHIHU_API_BASE"))
        or "https://developer.zhihu.com"
    )
    zhida_model = (
        _clean_config_string(zhihu.get("zhida_model"))
        or _clean_config_string(os.getenv("ZHIHU_ZHIDA_MODEL"))
        or "zhida-fast-1p5"
    )
    timeout_seconds = _optional_float(
        zhihu.get("timeout_seconds") if "timeout_seconds" in zhihu else os.getenv("ZHIHU_TIMEOUT_SECONDS"),
        30.0,
    )
    return ZhihuConfig(
        access_secret=access_secret,
        api_base=api_base.rstrip("/"),
        timeout_seconds=max(1.0, timeout_seconds),
        zhida_model=zhida_model,
    )


class ZhihuClient:
    def __init__(self, config: ZhihuConfig, http_client: Any | None = None) -> None:
        self.config = config
        self.http_client = http_client

    def _headers(self) -> dict[str, str]:
        if not self.config.access_secret:
            raise RuntimeError("ZHIHU_ACCESS_SECRET is not configured")
        return {
            "Authorization": f"Bearer {self.config.access_secret}",
            "X-Request-Timestamp": str(int(time.time())),
            "Content-Type": "application/json",
        }

    async def _request_json(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        headers = self._headers()
        client = self.http_client
        close_client = False
        if client is None:
            import httpx

            client = httpx.AsyncClient(timeout=self.config.timeout_seconds)
            close_client = True
        try:
            url = f"{self.config.api_base}{path}"
            if method == "GET":
                response = await client.get(url, params=params, headers=headers)
            else:
                response = await client.post(url, json=json_body, headers=headers)
            response.raise_for_status()
            payload = response.json()
            if not isinstance(payload, dict):
                raise RuntimeError("Zhihu API returned a non-object JSON response")
            return payload
        finally:
            if close_client:
                await client.aclose()

    @staticmethod
    def _normalize_items(payload: dict[str, Any], *, tool_name: str) -> dict[str, Any]:
        code = payload.get("Code", payload.get("code", 0))
        message = str(payload.get("Message", payload.get("message", "")) or "")
        if code not in (0, "0", None):
            return {"ok": False, "tool": tool_name, "code": code, "error": message or "Zhihu API error"}
        data = payload.get("Data", payload.get("data", {}))
        if not isinstance(data, dict):
            data = {}
        raw_items = data.get("Items", data.get("items", []))
        if not isinstance(raw_items, list):
            raw_items = []
        items: list[dict[str, Any]] = []
        for raw in raw_items:
            if not isinstance(raw, dict):
                continue
            item = {
                "title": str(raw.get("Title", raw.get("title", "")) or ""),
                "url": str(raw.get("Url", raw.get("url", "")) or ""),
                "content_type": str(raw.get("ContentType", raw.get("content_type", "")) or ""),
                "content_text": _clip_text(raw.get("ContentText", raw.get("Summary", raw.get("summary", "")))),
                "author_name": str(raw.get("AuthorName", raw.get("author_name", "")) or ""),
                "author_badge_text": str(raw.get("AuthorBadgeText", raw.get("author_badge_text", "")) or ""),
                "comment_count": raw.get("CommentCount", raw.get("comment_count", 0)),
                "vote_up_count": raw.get("VoteUpCount", raw.get("vote_up_count", 0)),
                "authority_level": str(raw.get("AuthorityLevel", raw.get("authority_level", "")) or ""),
                "thumbnail_url": str(raw.get("ThumbnailUrl", raw.get("thumbnail_url", "")) or ""),
                "edit_time": raw.get("EditTime", raw.get("edit_time")),
            }
            comments = raw.get("CommentInfoList", raw.get("comment_info_list", []))
            if isinstance(comments, list):
                item["comments"] = [
                    _clip_text(comment.get("Content") if isinstance(comment, dict) else comment, limit=300)
                    for comment in comments[:3]
                ]
            items.append(item)
        return {
            "ok": True,
            "tool": tool_name,
            "has_more": bool(data.get("HasMore", data.get("has_more", False))),
            "total": data.get("Total", data.get("total", len(items))),
            "empty_reason": str(data.get("EmptyReason", data.get("empty_reason", "")) or ""),
            "items": items,
        }

    async def zhihu_search(self, query: str, count: int = 10) -> dict[str, Any]:
        count = _bounded_int(count, 10, 1, 10)
        payload = await self._request_json(
            "GET",
            "/api/v1/content/zhihu_search",
            params={"Query": str(query or "").strip(), "Count": count},
        )
        result = self._normalize_items(payload, tool_name="zhihu_search")
        result.update({"query": str(query or "").strip(), "count": count})
        return result

    async def global_search(self, query: str, count: int = 10) -> dict[str, Any]:
        count = _bounded_int(count, 10, 1, 20)
        payload = await self._request_json(
            "GET",
            "/api/v1/content/global_search",
            params={"Query": str(query or "").strip(), "Count": count},
        )
        result = self._normalize_items(payload, tool_name="global_search")
        result.update({"query": str(query or "").strip(), "count": count})
        return result

    async def hot_list(self, limit: int = 30) -> dict[str, Any]:
        limit = _bounded_int(limit, 30, 1, 30)
        payload = await self._request_json("GET", "/api/v1/content/hot_list", params={"Limit": limit})
        result = self._normalize_items(payload, tool_name="hot_list")
        result.update({"limit": limit})
        return result

    async def zhida(self, query: str, model: str = "") -> dict[str, Any]:
        selected_model = str(model or "").strip() or self.config.zhida_model
        payload = await self._request_json(
            "POST",
            "/v1/chat/completions",
            json_body={
                "model": selected_model,
                "messages": [{"role": "user", "content": str(query or "").strip()}],
                "stream": False,
            },
        )
        error = payload.get("error")
        if isinstance(error, dict):
            return {
                "ok": False,
                "tool": "zhida",
                "model": selected_model,
                "error": str(error.get("message") or "Zhihu Zhida API error"),
                "code": error.get("code"),
            }
        choices = payload.get("choices", [])
        message = choices[0].get("message", {}) if choices and isinstance(choices[0], dict) else {}
        if not isinstance(message, dict):
            message = {}
        return {
            "ok": True,
            "tool": "zhida",
            "model": selected_model,
            "query": str(query or "").strip(),
            "answer": str(message.get("content") or "").strip(),
            "reasoning": _clip_text(message.get("reasoning_content"), limit=2000),
        }


class ZhihuToolExecutor:
    def __init__(self, client: ZhihuClient) -> None:
        self.client = client

    async def execute(self, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
        args = dict(arguments or {})
        try:
            if name == "zhihu_search":
                return await self.client.zhihu_search(str(args.get("query") or ""), args.get("count", 10))
            if name == "global_search":
                return await self.client.global_search(str(args.get("query") or ""), args.get("count", 10))
            if name == "hot_list":
                return await self.client.hot_list(args.get("limit", 30))
            if name == "zhida":
                return await self.client.zhida(str(args.get("query") or ""), str(args.get("model") or ""))
        except Exception as exc:
            return {"ok": False, "tool": name, "error": str(exc)}
        return {"ok": False, "tool": name, "error": f"unsupported tool: {name}"}
