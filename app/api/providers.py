from __future__ import annotations

from fastapi import APIRouter, Depends

from app.api.deps import get_container, require_api_token
from app.dependencies import AppContainer

router = APIRouter(
    prefix="/providers", tags=["providers"], dependencies=[Depends(require_api_token)]
)


@router.get("/health")
async def provider_health(container: AppContainer = Depends(get_container)) -> dict:
    health = await container.model_router.health()
    return {"providers": [item.model_dump() for item in health]}


@router.get("/codex/login-status")
async def codex_login_status(container: AppContainer = Depends(get_container)) -> dict:
    health = await container.model_router.codex_login_status()
    return health.model_dump()
