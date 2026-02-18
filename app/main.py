from __future__ import annotations

from pathlib import Path

from fastapi import FastAPI
from fastapi.responses import FileResponse

from app.api import automations, feishu, permissions, providers, runtime, sessions, todos
from app.config import get_settings
from app.dependencies import AppContainer
from app.logging_config import configure_logging

configure_logging()
settings = get_settings()

app = FastAPI(title=settings.app_name)

api_prefix = settings.api_prefix
app.include_router(feishu.router, prefix=api_prefix)
app.include_router(runtime.router, prefix=api_prefix)
app.include_router(todos.router, prefix=api_prefix)
app.include_router(automations.router, prefix=api_prefix)
app.include_router(sessions.router, prefix=api_prefix)
app.include_router(providers.router, prefix=api_prefix)
app.include_router(permissions.router, prefix=api_prefix)


@app.on_event("startup")
async def on_startup() -> None:
    container = AppContainer.build(settings)
    app.state.container = container
    await container.startup()


@app.on_event("shutdown")
async def on_shutdown() -> None:
    container: AppContainer = app.state.container
    await container.shutdown()


@app.get("/health")
async def health() -> dict:
    return {"ok": True, "name": settings.app_name}


@app.get("/")
async def dashboard() -> FileResponse:
    html_path = Path(__file__).resolve().parent / "static" / "dashboard.html"
    return FileResponse(str(html_path))
