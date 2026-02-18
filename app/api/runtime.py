from __future__ import annotations

from fastapi import APIRouter, Depends

from app.api.deps import get_container, require_api_token
from app.dependencies import AppContainer

router = APIRouter(prefix="/runtime", tags=["runtime"], dependencies=[Depends(require_api_token)])


@router.post("/pause")
async def pause_runtime(
    payload: dict,
    container: AppContainer = Depends(get_container),
) -> dict:
    reason = str(payload.get("reason") or "")
    await container.runtime_control.pause(reason)
    return {"ok": True, "paused": True, "reason": reason}


@router.post("/resume")
async def resume_runtime(container: AppContainer = Depends(get_container)) -> dict:
    await container.runtime_control.resume()
    return {"ok": True, "paused": False}


@router.get("/status")
async def runtime_status(container: AppContainer = Depends(get_container)) -> dict:
    return await container.runtime_control.status()
