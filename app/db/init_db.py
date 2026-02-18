from __future__ import annotations

from sqlalchemy.ext.asyncio import AsyncEngine

from app.db import models  # noqa: F401
from app.db.base import Base


async def init_db(engine: AsyncEngine) -> None:
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
