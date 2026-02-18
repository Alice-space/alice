from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from sqlalchemy.ext.asyncio import AsyncEngine, AsyncSession, async_sessionmaker

from app.config import Settings
from app.db.init_db import init_db
from app.db.session import create_session_factory
from app.memory.store import FileMemoryStore
from app.providers.router import ModelRouter
from app.runtime.control import RuntimeControl
from app.runtime.orchestrator import RuntimeOrchestrator
from app.runtime.stream import SessionStreamHub
from app.services.feishu import FeishuService
from app.services.permissions import PermissionService
from app.tools.registry import ToolRegistry
from app.triggers.scheduler import AutomationScheduler
from app.triggers.todo_worker import TodoDrainWorker


@dataclass(slots=True)
class AppContainer:
    settings: Settings
    engine: AsyncEngine
    session_factory: async_sessionmaker[AsyncSession]
    feishu_service: FeishuService
    permission_service: PermissionService
    memory_store: FileMemoryStore
    model_router: ModelRouter
    tool_registry: ToolRegistry
    stream_hub: SessionStreamHub
    runtime_control: RuntimeControl
    orchestrator: RuntimeOrchestrator
    automation_scheduler: AutomationScheduler
    todo_worker: TodoDrainWorker

    @classmethod
    def build(cls, settings: Settings) -> "AppContainer":
        engine, session_factory = create_session_factory(settings.resolved_database_url)
        feishu_service = FeishuService(settings)
        permission_service = PermissionService(settings)
        memory_store = FileMemoryStore(settings)
        model_router = ModelRouter(settings)
        stream_hub = SessionStreamHub()
        runtime_control = RuntimeControl()
        tool_registry = ToolRegistry(
            settings=settings,
            feishu_service=feishu_service,
            permission_service=permission_service,
            memory_store=memory_store,
        )
        orchestrator = RuntimeOrchestrator(
            settings=settings,
            session_factory=session_factory,
            model_router=model_router,
            tool_registry=tool_registry,
            memory_store=memory_store,
            stream_hub=stream_hub,
            runtime_control=runtime_control,
            feishu_service=feishu_service,
        )
        automation_scheduler = AutomationScheduler(settings, session_factory, orchestrator)
        todo_worker = TodoDrainWorker(session_factory, orchestrator)

        return cls(
            settings=settings,
            engine=engine,
            session_factory=session_factory,
            feishu_service=feishu_service,
            permission_service=permission_service,
            memory_store=memory_store,
            model_router=model_router,
            tool_registry=tool_registry,
            stream_hub=stream_hub,
            runtime_control=runtime_control,
            orchestrator=orchestrator,
            automation_scheduler=automation_scheduler,
            todo_worker=todo_worker,
        )

    async def startup(self) -> None:
        self._ensure_state_directories()
        await init_db(self.engine)
        self.memory_store.ensure_files()
        self.automation_scheduler.start()
        await self.automation_scheduler.load_from_db()
        await self.todo_worker.recover_on_startup()

    async def shutdown(self) -> None:
        self.automation_scheduler.shutdown()
        await self.engine.dispose()

    def _ensure_state_directories(self) -> None:
        self.settings.resolved_state_dir.mkdir(parents=True, exist_ok=True)
        self.settings.resolved_memory_dir.mkdir(parents=True, exist_ok=True)
        self.settings.resolved_skills_dir.mkdir(parents=True, exist_ok=True)

        if self.settings.sync_database_url.startswith("sqlite:///"):
            db_path = Path(self.settings.sync_database_url.replace("sqlite:///", "", 1))
            db_path.parent.mkdir(parents=True, exist_ok=True)
