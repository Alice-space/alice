from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.deps import get_container, get_db, require_api_token
from app.dependencies import AppContainer

router = APIRouter(
    prefix="/permissions", tags=["permissions"], dependencies=[Depends(require_api_token)]
)


class PermissionDecisionRequest(BaseModel):
    note: str = ""


@router.post("/{request_id}/approve")
async def approve_permission(
    request_id: str,
    req: PermissionDecisionRequest,
    db: AsyncSession = Depends(get_db),
    container: AppContainer = Depends(get_container),
) -> dict:
    row = await container.permission_service.approve(db, request_id, note=req.note)
    if not row:
        raise HTTPException(status_code=404, detail="permission request not found")
    await db.commit()
    return {
        "id": row.id,
        "status": row.status,
        "tool_name": row.tool_name,
        "resolved_at": row.resolved_at.isoformat() if row.resolved_at else None,
    }


@router.post("/{request_id}/reject")
async def reject_permission(
    request_id: str,
    req: PermissionDecisionRequest,
    db: AsyncSession = Depends(get_db),
    container: AppContainer = Depends(get_container),
) -> dict:
    row = await container.permission_service.reject(db, request_id, note=req.note)
    if not row:
        raise HTTPException(status_code=404, detail="permission request not found")
    await db.commit()
    return {
        "id": row.id,
        "status": row.status,
        "tool_name": row.tool_name,
        "resolved_at": row.resolved_at.isoformat() if row.resolved_at else None,
    }
