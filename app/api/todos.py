from __future__ import annotations

from typing import Any

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, Field
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.deps import get_container, get_db, require_api_token
from app.db.models import TodoRecord, TodoStatus
from app.dependencies import AppContainer

router = APIRouter(prefix="/todos", tags=["todos"], dependencies=[Depends(require_api_token)])


class TodoCreateRequest(BaseModel):
    title: str = Field(min_length=1, max_length=256)
    description: str = ""
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
    row = TodoRecord(
        title=req.title,
        description=req.description,
        priority=req.priority,
        metadata_json=req.metadata,
    )
    db.add(row)
    await db.commit()
    await db.refresh(row)
    await container.todo_worker.trigger_if_needed()
    return _to_dict(row)


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
