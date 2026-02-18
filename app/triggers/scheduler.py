from __future__ import annotations

import asyncio
import logging
from datetime import datetime

from apscheduler.jobstores.sqlalchemy import SQLAlchemyJobStore
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from apscheduler.triggers.cron import CronTrigger
from apscheduler.triggers.interval import IntervalTrigger
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.config import Settings
from app.db.models import AutomationJobRecord, AutomationStatus
from app.runtime.orchestrator import RuntimeOrchestrator
from app.runtime.types import RuntimeTriggerEvent

logger = logging.getLogger(__name__)


class AutomationScheduler:
    def __init__(
        self,
        settings: Settings,
        session_factory: async_sessionmaker[AsyncSession],
        orchestrator: RuntimeOrchestrator,
    ) -> None:
        self.settings = settings
        self.session_factory = session_factory
        self.orchestrator = orchestrator
        self.scheduler = AsyncIOScheduler(
            jobstores={"default": SQLAlchemyJobStore(url=settings.sync_database_url)}
        )

    def start(self) -> None:
        if not self.settings.scheduler_enabled:
            return
        self.scheduler.start()
        logger.info("automation scheduler started")

    def shutdown(self) -> None:
        if self.scheduler.running:
            self.scheduler.shutdown(wait=False)

    async def load_from_db(self) -> None:
        async with self.session_factory() as db:
            rows = await db.execute(select(AutomationJobRecord))
            jobs = rows.scalars().all()

        for job in jobs:
            if job.status == AutomationStatus.active.value:
                self.upsert_job(job)

    def upsert_job(self, job: AutomationJobRecord) -> None:
        trigger = self._build_trigger(job.schedule)
        self.scheduler.add_job(
            self._run_job_wrapper,
            trigger=trigger,
            args=[job.id],
            id=f"automation-{job.id}",
            replace_existing=True,
            misfire_grace_time=60,
        )

    def remove_job(self, job_id: int) -> None:
        self.scheduler.remove_job(f"automation-{job_id}")

    async def _run_job_wrapper(self, job_id: int) -> None:
        async with self.session_factory() as db:
            record = await db.get(AutomationJobRecord, job_id)
            if not record or record.status != AutomationStatus.active.value:
                return
            record.last_run_at = datetime.utcnow()
            await db.commit()
            prompt = record.prompt

        event = RuntimeTriggerEvent(
            trigger_type="schedule_fire",
            text=prompt,
            metadata={"automation_job_id": job_id},
        )
        asyncio.create_task(self.orchestrator.handle_trigger(event))

    def _build_trigger(self, schedule: str):
        # Supported formats:
        # - interval:<seconds>, e.g. interval:3600
        # - cron:<five fields>, e.g. cron:0 * * * *
        if schedule.startswith("interval:"):
            seconds = int(schedule.split(":", 1)[1])
            return IntervalTrigger(seconds=max(seconds, 1))

        if schedule.startswith("cron:"):
            expr = schedule.split(":", 1)[1].strip()
            minute, hour, day, month, day_of_week = expr.split()
            return CronTrigger(
                minute=minute,
                hour=hour,
                day=day,
                month=month,
                day_of_week=day_of_week,
                timezone=self.settings.timezone,
            )

        # Default fallback: run every hour.
        return IntervalTrigger(seconds=3600)
