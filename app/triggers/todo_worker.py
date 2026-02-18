from __future__ import annotations

import asyncio
import logging

from sqlalchemy import asc, select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.db.models import TodoRecord, TodoStatus
from app.runtime.orchestrator import RuntimeOrchestrator
from app.runtime.types import RuntimeTriggerEvent

logger = logging.getLogger(__name__)


class TodoDrainWorker:
    def __init__(
        self,
        session_factory: async_sessionmaker[AsyncSession],
        orchestrator: RuntimeOrchestrator,
    ) -> None:
        self.session_factory = session_factory
        self.orchestrator = orchestrator
        self._lock = asyncio.Lock()
        self._running = False

    async def recover_on_startup(self) -> None:
        async with self.session_factory() as db:
            pending_count = await self._count_pending(db)
        if pending_count > 0:
            await self.trigger_if_needed()

    async def trigger_if_needed(self) -> None:
        async with self._lock:
            if self._running:
                return
            async with self.session_factory() as db:
                pending_count = await self._count_pending(db)
                if pending_count <= 0:
                    return
            self._running = True
            asyncio.create_task(self._run())

    async def _run(self) -> None:
        try:
            while True:
                async with self.session_factory() as db:
                    row = await db.execute(
                        select(TodoRecord)
                        .where(TodoRecord.status == TodoStatus.pending.value)
                        .order_by(asc(TodoRecord.created_at))
                        .limit(1)
                    )
                    todo = row.scalars().first()
                    if not todo:
                        break
                    todo.status = TodoStatus.in_progress.value
                    await db.commit()
                    todo_snapshot = {
                        "id": todo.id,
                        "title": todo.title,
                        "description": todo.description,
                    }

                event = RuntimeTriggerEvent(
                    trigger_type="todo_non_empty",
                    text=f"Process todo #{todo_snapshot['id']}: {todo_snapshot['title']}\n{todo_snapshot['description']}",
                    metadata={"todo_id": todo_snapshot["id"]},
                )
                session_id = await self.orchestrator.handle_trigger(event)

                async with self.session_factory() as db:
                    todo = await db.get(TodoRecord, todo_snapshot["id"])
                    if not todo:
                        continue
                    if session_id is None:
                        todo.status = TodoStatus.pending.value
                    else:
                        todo.status = TodoStatus.done.value
                    await db.commit()
        except Exception:  # noqa: BLE001
            logger.exception("todo drain worker crashed")
        finally:
            async with self._lock:
                self._running = False

    async def _count_pending(self, db: AsyncSession) -> int:
        rows = await db.execute(
            select(TodoRecord).where(TodoRecord.status == TodoStatus.pending.value)
        )
        return len(rows.scalars().all())
