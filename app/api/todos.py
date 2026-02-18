from __future__ import annotations

import json
from collections.abc import AsyncIterator
from typing import Any

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.deps import get_container, get_db, require_api_token
from app.db.models import TodoRecord, TodoStatus
from app.dependencies import AppContainer

router = APIRouter(prefix="/todos", tags=["todos"], dependencies=[Depends(require_api_token)])


class TodoCreateRequest(BaseModel):
    title: str | None = Field(default=None, max_length=256)
    description: str = ""
    priority: int = 0
    metadata: dict[str, Any] = Field(default_factory=dict)


class TodoDialogRequest(BaseModel):
    message: str = Field(min_length=1, max_length=4000)
    priority: int = 0
    metadata: dict[str, Any] = Field(default_factory=dict)


class TodoUpdateRequest(BaseModel):
    title: str | None = None
    description: str | None = None
    status: str | None = None
    priority: int | None = None


@router.post("")
async def create_todo(
    req: TodoCreateRequest,
    db: AsyncSession = Depends(get_db),
    container: AppContainer = Depends(get_container),
) -> dict:
    requested_title = (req.title or "").strip()
    description = req.description.strip()
    if not requested_title and not description:
        raise HTTPException(status_code=400, detail="title or description is required")

    title_source = "manual"
    title = requested_title
    if not title:
        title, title_source = await container.todo_title_generator.summarize(description)

    row = TodoRecord(
        title=title,
        description=description,
        priority=req.priority,
        metadata_json={**req.metadata, "title_source": title_source},
    )
    db.add(row)
    await db.commit()
    await db.refresh(row)
    await container.todo_worker.trigger_if_needed()
    return _to_dict(row)


@router.post("/dialog")
async def create_todo_from_dialog(
    req: TodoDialogRequest,
    db: AsyncSession = Depends(get_db),
    container: AppContainer = Depends(get_container),
) -> dict:
    row, title_source = await _create_dialog_todo(req, db, container)
    await container.todo_worker.trigger_if_needed()
    return {
        "reply": f"Created todo: {row.title}",
        "todo": _to_dict(row),
        "title_source": title_source,
    }


@router.post("/dialog/stream")
async def create_todo_from_dialog_stream(
    req: TodoDialogRequest,
    db: AsyncSession = Depends(get_db),
    container: AppContainer = Depends(get_container),
) -> StreamingResponse:
    async def event_stream() -> AsyncIterator[str]:
        yield _sse("status", {"phase": "received", "message": "Request received"})
        yield _sse("status", {"phase": "summarizing", "message": "Generating todo title..."})
        try:
            row, title_source = await _create_dialog_todo(req, db, container)
            await container.todo_worker.trigger_if_needed()
            yield _sse(
                "title",
                {
                    "title": row.title,
                    "title_source": title_source,
                },
            )
            yield _sse(
                "created",
                {
                    "reply": f"Created todo: {row.title}",
                    "todo": _to_dict(row),
                },
            )
            yield _sse("done", {"ok": True})
        except Exception as exc:  # noqa: BLE001
            yield _sse("error", {"error": str(exc)})

    return StreamingResponse(
        event_stream(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )


@router.patch("/{todo_id}")
async def update_todo(
    todo_id: int,
    req: TodoUpdateRequest,
    db: AsyncSession = Depends(get_db),
    container: AppContainer = Depends(get_container),
) -> dict:
    row = await db.get(TodoRecord, todo_id)
    if not row:
        raise HTTPException(status_code=404, detail="todo not found")

    if req.title is not None:
        row.title = req.title
    if req.description is not None:
        row.description = req.description
    if req.status is not None:
        row.status = req.status
    if req.priority is not None:
        row.priority = req.priority

    await db.commit()
    await db.refresh(row)
    if row.status == TodoStatus.pending.value:
        await container.todo_worker.trigger_if_needed()
    return _to_dict(row)


@router.get("")
async def list_todos(status: str | None = None, db: AsyncSession = Depends(get_db)) -> dict:
    stmt = select(TodoRecord).order_by(TodoRecord.created_at.desc()).limit(200)
    if status:
        stmt = stmt.where(TodoRecord.status == status)
    rows = (await db.execute(stmt)).scalars().all()
    return {"items": [_to_dict(row) for row in rows]}


def _to_dict(row: TodoRecord) -> dict[str, Any]:
    return {
        "id": row.id,
        "title": row.title,
        "description": row.description,
        "status": row.status,
        "priority": row.priority,
        "metadata": row.metadata_json,
        "created_at": row.created_at.isoformat() if row.created_at else None,
        "updated_at": row.updated_at.isoformat() if row.updated_at else None,
    }


async def _create_dialog_todo(
    req: TodoDialogRequest, db: AsyncSession, container: AppContainer
) -> tuple[TodoRecord, str]:
    title, title_source = await container.todo_title_generator.summarize(req.message)
    metadata = {**req.metadata, "title_source": title_source, "via": "dialog"}
    row = TodoRecord(
        title=title,
        description=req.message.strip(),
        priority=req.priority,
        metadata_json=metadata,
    )
    db.add(row)
    await db.commit()
    await db.refresh(row)
    return row, title_source


def _sse(event: str, payload: dict[str, Any]) -> str:
    return f"event: {event}\ndata: {json.dumps(payload, ensure_ascii=False)}\n\n"
