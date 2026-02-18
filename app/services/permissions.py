from __future__ import annotations

from datetime import datetime, timedelta

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.config import Settings
from app.db.models import PermissionRequestRecord, PermissionStatus


class PermissionService:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings

    async def create_request(
        self,
        db: AsyncSession,
        *,
        session_id: str,
        tool_name: str,
        payload: dict,
    ) -> PermissionRequestRecord:
        expires_at = datetime.utcnow() + timedelta(seconds=self.settings.permission_timeout_seconds)
        request = PermissionRequestRecord(
            session_id=session_id,
            tool_name=tool_name,
            payload_json=payload,
            status=PermissionStatus.pending.value,
            expires_at=expires_at,
        )
        db.add(request)
        await db.flush()
        return request

    async def approve(
        self, db: AsyncSession, request_id: str, note: str = ""
    ) -> PermissionRequestRecord | None:
        request = await db.get(PermissionRequestRecord, request_id)
        if not request:
            return None
        request.status = PermissionStatus.approved.value
        request.resolved_at = datetime.utcnow()
        request.decision_note = note
        await db.flush()
        return request

    async def reject(
        self, db: AsyncSession, request_id: str, note: str = ""
    ) -> PermissionRequestRecord | None:
        request = await db.get(PermissionRequestRecord, request_id)
        if not request:
            return None
        request.status = PermissionStatus.rejected.value
        request.resolved_at = datetime.utcnow()
        request.decision_note = note
        await db.flush()
        return request

    async def get(self, db: AsyncSession, request_id: str) -> PermissionRequestRecord | None:
        return await db.get(PermissionRequestRecord, request_id)

    async def list_pending(self, db: AsyncSession) -> list[PermissionRequestRecord]:
        rows = await db.execute(
            select(PermissionRequestRecord).where(
                PermissionRequestRecord.status == PermissionStatus.pending.value
            )
        )
        return list(rows.scalars().all())
