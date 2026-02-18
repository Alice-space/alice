from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass
from typing import Any, Awaitable, Callable

import httpx
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.config import Settings
from app.db.models import TodoRecord
from app.memory.store import FileMemoryStore
from app.runtime.types import ToolExecutionResult
from app.services.feishu import FeishuService
from app.services.permissions import PermissionService
from app.services.todo_title import TodoTitleGenerator

logger = logging.getLogger(__name__)

ToolHandler = Callable[[AsyncSession, dict[str, Any]], Awaitable[dict[str, Any]]]


@dataclass(slots=True)
class ToolSpec:
    name: str
    trusted: bool
    handler: ToolHandler


class ToolRegistry:
    def __init__(
        self,
        settings: Settings,
        feishu_service: FeishuService,
        permission_service: PermissionService,
        todo_title_generator: TodoTitleGenerator,
        memory_store: FileMemoryStore,
    ) -> None:
        self.settings = settings
        self.feishu_service = feishu_service
        self.permission_service = permission_service
        self.todo_title_generator = todo_title_generator
        self.memory_store = memory_store
        self._tools: dict[str, ToolSpec] = {
            "feishu.send_message": ToolSpec(
                "feishu.send_message", True, self._handle_feishu_send_message
            ),
            "todo.create": ToolSpec("todo.create", True, self._handle_todo_create),
            "todo.update": ToolSpec("todo.update", True, self._handle_todo_update),
            "todo.list": ToolSpec("todo.list", True, self._handle_todo_list),
            "http.request": ToolSpec("http.request", True, self._handle_http_request),
        }

    def describe_tools(self) -> list[dict[str, Any]]:
        return [
            {
                "name": spec.name,
                "trusted": spec.trusted,
                "approval_mode": self.settings.approval_mode,
            }
            for spec in self._tools.values()
        ]

    async def execute(
        self,
        db: AsyncSession,
        *,
        session_id: str,
        tool_name: str,
        arguments: dict[str, Any],
    ) -> ToolExecutionResult:
        spec = self._tools.get(tool_name)
        if not spec:
            return ToolExecutionResult(ok=False, error=f"unknown tool: {tool_name}")

        if self._needs_approval(spec, arguments):
            request = await self.permission_service.create_request(
                db,
                session_id=session_id,
                tool_name=tool_name,
                payload=arguments,
            )
            return ToolExecutionResult(
                ok=False,
                error="approval required",
                requires_approval=True,
                permission_request_id=request.id,
                output={"permission_request_id": request.id},
            )

        max_attempts = max(self.settings.tool_retry_max_attempts, 1)
        last_error = ""
        for attempt in range(1, max_attempts + 1):
            try:
                output = await spec.handler(db, arguments)
                return ToolExecutionResult(ok=True, output=output)
            except Exception as exc:  # noqa: BLE001
                last_error = str(exc)
                logger.warning("tool %s attempt=%s failed: %s", tool_name, attempt, last_error)
                if attempt < max_attempts:
                    await asyncio.sleep(2 ** (attempt - 1))

        return ToolExecutionResult(ok=False, error=last_error)

    def _needs_approval(self, spec: ToolSpec, arguments: dict[str, Any]) -> bool:
        if spec.name in set(self.settings.approval_required_tools):
            return True

        if spec.name == "http.request" and self.settings.http_write_requires_approval:
            method = str(arguments.get("method", "GET")).upper()
            if method in {"POST", "PUT", "PATCH", "DELETE"}:
                return True

        mode = self.settings.approval_mode
        if mode == "auto_all":
            return False
        if mode == "trusted_only":
            return spec.name not in set(self.settings.trusted_tools)
        if mode == "explicit_only":
            return True
        return False

    async def _handle_feishu_send_message(
        self, _: AsyncSession, arguments: dict[str, Any]
    ) -> dict[str, Any]:
        receive_id = str(arguments.get("receive_id") or "")
        text = str(arguments.get("text") or "")
        receive_id_type = str(arguments.get("receive_id_type") or "chat_id")
        if not receive_id or not text:
            raise ValueError("feishu.send_message requires receive_id and text")
        return await self.feishu_service.send_message(
            receive_id=receive_id, text=text, receive_id_type=receive_id_type
        )

    async def _handle_todo_create(
        self, db: AsyncSession, arguments: dict[str, Any]
    ) -> dict[str, Any]:
        title = str(arguments.get("title") or "").strip()
        description = str(arguments.get("description") or "")
        title_source = "manual"
        if not title:
            summary_seed = description or str(arguments.get("raw_text") or "")
            title, title_source = await self.todo_title_generator.summarize(summary_seed)
        todo = TodoRecord(
            title=title,
            description=description,
            priority=int(arguments.get("priority") or 0),
            metadata_json={
                **(arguments.get("metadata") or {}),
                "title_source": title_source,
            },
        )
        db.add(todo)
        await db.flush()
        return {
            "id": todo.id,
            "title": todo.title,
            "description": todo.description,
            "status": todo.status,
            "priority": todo.priority,
            "title_source": title_source,
        }

    async def _handle_todo_update(
        self, db: AsyncSession, arguments: dict[str, Any]
    ) -> dict[str, Any]:
        todo_id = arguments.get("id")
        if todo_id is None:
            raise ValueError("todo.update requires id")
        todo = await db.get(TodoRecord, int(todo_id))
        if not todo:
            raise ValueError(f"todo not found: {todo_id}")

        if "title" in arguments:
            todo.title = str(arguments["title"])
        if "description" in arguments:
            todo.description = str(arguments["description"])
        if "status" in arguments:
            todo.status = str(arguments["status"])
        if "priority" in arguments:
            todo.priority = int(arguments["priority"])

        await db.flush()
        return {
            "id": todo.id,
            "title": todo.title,
            "description": todo.description,
            "status": todo.status,
            "priority": todo.priority,
        }

    async def _handle_todo_list(
        self, db: AsyncSession, arguments: dict[str, Any]
    ) -> dict[str, Any]:
        status = arguments.get("status")
        stmt = select(TodoRecord).order_by(TodoRecord.created_at.desc()).limit(100)
        if status:
            stmt = stmt.where(TodoRecord.status == str(status))
        rows = (await db.execute(stmt)).scalars().all()
        items = [
            {
                "id": row.id,
                "title": row.title,
                "description": row.description,
                "status": row.status,
                "priority": row.priority,
                "created_at": row.created_at.isoformat() if row.created_at else None,
            }
            for row in rows
        ]
        return {"items": items}

    async def _handle_http_request(
        self, _: AsyncSession, arguments: dict[str, Any]
    ) -> dict[str, Any]:
        method = str(arguments.get("method", "GET")).upper()
        url = str(arguments.get("url") or "").strip()
        if not url:
            raise ValueError("http.request requires url")

        headers = arguments.get("headers") or {}
        body = arguments.get("body")
        timeout = float(arguments.get("timeout_seconds") or 20)

        async with httpx.AsyncClient(timeout=timeout) as client:
            response = await client.request(method=method, url=url, headers=headers, json=body)

        text = response.text
        if len(text) > 4000:
            text = text[:4000] + "...<truncated>"

        return {
            "status_code": response.status_code,
            "headers": dict(response.headers),
            "body": text,
        }
