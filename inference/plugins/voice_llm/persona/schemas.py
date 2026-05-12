from __future__ import annotations

from dataclasses import dataclass, fields
from datetime import datetime
from typing import Any, Literal


TaskStatus = Literal["queued", "running", "waiting_user", "completed", "failed", "cancelled"]


def _dump_value(value: Any, *, exclude_none: bool) -> Any:
    if exclude_none and value is None:
        return None
    if isinstance(value, datetime):
        return value.isoformat()
    if isinstance(value, list):
        return [_dump_value(item, exclude_none=exclude_none) for item in value]
    if isinstance(value, dict):
        return {
            key: _dump_value(item, exclude_none=exclude_none)
            for key, item in value.items()
            if not exclude_none or item is not None
        }
    return value


class ModelDumpMixin:
    def model_dump(self, *args: Any, exclude_none: bool = False, **kwargs: Any) -> dict[str, Any]:
        dumped: dict[str, Any] = {}
        for item in fields(self):
            value = getattr(self, item.name)
            if exclude_none and value is None:
                continue
            dumped[item.name] = _dump_value(value, exclude_none=exclude_none)
        return dumped


@dataclass
class Task(ModelDumpMixin):
    id: str
    session_id: str
    title: str
    user_request: str
    character_id: str | None = None
    status: TaskStatus = "queued"
    progress: int = 0
    result_summary: str | None = None
    locale: str | None = None
    metadata: dict[str, Any] | None = None
    created_at: datetime | None = None
    updated_at: datetime | None = None
    finished_at: datetime | None = None


@dataclass
class TaskEvent(ModelDumpMixin):
    event_type: str
    status: TaskStatus = "running"
    message: str = ""
    progress: int = 0
    payload: dict[str, Any] | None = None
    task_id: str | None = None
    seq: int | None = None
    created_at: datetime | None = None


@dataclass
class ArtifactRequest(ModelDumpMixin):
    title: str
    content: str
    type: str = "markdown"
    mime_type: str = "text/markdown; charset=utf-8"
    metadata: dict[str, Any] | None = None


@dataclass
class Artifact(ModelDumpMixin):
    id: str
    task_id: str
    title: str
    content: str
    type: str = "markdown"
    mime_type: str = "text/markdown; charset=utf-8"
    metadata: dict[str, Any] | None = None
    created_at: datetime | None = None
