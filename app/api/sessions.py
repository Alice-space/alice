from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import StreamingResponse
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.api.deps import get_container, get_db, require_api_token
from app.db.models import MessageRecord, RuntimeEventRecord, SessionRecord, TurnRecord
from app.dependencies import AppContainer

router = APIRouter(prefix="/sessions", tags=["sessions"], dependencies=[Depends(require_api_token)])


@router.get("/{session_id}")
async def get_session(session_id: str, db: AsyncSession = Depends(get_db)) -> dict:
    session = await db.get(SessionRecord, session_id)
    if not session:
        raise HTTPException(status_code=404, detail="session not found")

    messages = (
        (
            await db.execute(
                select(MessageRecord)
                .where(MessageRecord.session_id == session_id)
                .order_by(MessageRecord.id.asc())
            )
        )
        .scalars()
        .all()
    )

    turns = (
        (
            await db.execute(
                select(TurnRecord)
                .where(TurnRecord.session_id == session_id)
                .order_by(TurnRecord.id.asc())
            )
        )
        .scalars()
        .all()
    )

    events = (
        (
            await db.execute(
                select(RuntimeEventRecord)
                .where(RuntimeEventRecord.session_id == session_id)
                .order_by(RuntimeEventRecord.id.asc())
            )
        )
        .scalars()
        .all()
    )

    return {
        "session": {
            "id": session.id,
            "trigger_source": session.trigger_source,
            "status": session.status,
            "provider_used": session.provider_used,
            "fallback_reason": session.fallback_reason,
            "metadata": session.metadata_json,
            "created_at": session.created_at.isoformat() if session.created_at else None,
            "updated_at": session.updated_at.isoformat() if session.updated_at else None,
        },
        "turns": [
            {
                "id": row.id,
                "step_index": row.step_index,
                "provider_used": row.provider_used,
                "action_type": row.action_type,
                "response_text": row.response_text,
                "created_at": row.created_at.isoformat() if row.created_at else None,
            }
            for row in turns
        ],
        "messages": [
            {
                "id": row.id,
                "role": row.role,
                "content": row.content,
                "metadata": row.metadata_json,
                "created_at": row.created_at.isoformat() if row.created_at else None,
            }
            for row in messages
        ],
        "events": [
            {
                "id": row.id,
                "event_type": row.event_type,
                "payload": row.payload_json,
                "created_at": row.created_at.isoformat() if row.created_at else None,
            }
            for row in events
        ],
    }


@router.get("/{session_id}/stream")
async def stream_session(
    session_id: str,
    container: AppContainer = Depends(get_container),
) -> StreamingResponse:
    return StreamingResponse(
        container.stream_hub.sse_generator(session_id),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "Connection": "keep-alive"},
    )
