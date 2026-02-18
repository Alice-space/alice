from __future__ import annotations

from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, Field
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.deps import get_container, get_db, require_api_token
from app.db.models import AutomationJobRecord, AutomationStatus
from app.dependencies import AppContainer

router = APIRouter(
    prefix="/automations", tags=["automations"], dependencies=[Depends(require_api_token)]
)


class AutomationCreateRequest(BaseModel):
    name: str = Field(min_length=1, max_length=128)
    prompt: str = Field(min_length=1)
    schedule: str = Field(min_length=1, description="interval:<seconds> or cron:<5 fields>")
    status: str = AutomationStatus.active.value


class AutomationUpdateRequest(BaseModel):
    name: str | None = None
    prompt: str | None = None
    schedule: str | None = None
    status: str | None = None


@router.post("")
async def create_automation(
    req: AutomationCreateRequest,
    db: AsyncSession = Depends(get_db),
    container: AppContainer = Depends(get_container),
) -> dict:
    row = AutomationJobRecord(
        name=req.name,
        prompt=req.prompt,
        schedule=req.schedule,
        status=req.status,
    )
    db.add(row)
    await db.commit()
    await db.refresh(row)

    if row.status == AutomationStatus.active.value:
        container.automation_scheduler.upsert_job(row)

    return _to_dict(row)


@router.patch("/{job_id}")
async def update_automation(
    job_id: int,
    req: AutomationUpdateRequest,
    db: AsyncSession = Depends(get_db),
    container: AppContainer = Depends(get_container),
) -> dict:
    row = await db.get(AutomationJobRecord, job_id)
    if not row:
        raise HTTPException(status_code=404, detail="automation not found")

    if req.name is not None:
        row.name = req.name
    if req.prompt is not None:
        row.prompt = req.prompt
    if req.schedule is not None:
        row.schedule = req.schedule
    if req.status is not None:
        row.status = req.status

    row.updated_at = datetime.utcnow()
    await db.commit()
    await db.refresh(row)

    if row.status == AutomationStatus.active.value:
        container.automation_scheduler.upsert_job(row)
    else:
        container.automation_scheduler.remove_job(row.id)

    return _to_dict(row)


def _to_dict(row: AutomationJobRecord) -> dict:
    return {
        "id": row.id,
        "name": row.name,
        "prompt": row.prompt,
        "schedule": row.schedule,
        "status": row.status,
        "last_run_at": row.last_run_at.isoformat() if row.last_run_at else None,
        "next_run_at": row.next_run_at.isoformat() if row.next_run_at else None,
        "created_at": row.created_at.isoformat() if row.created_at else None,
        "updated_at": row.updated_at.isoformat() if row.updated_at else None,
    }
