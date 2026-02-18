from __future__ import annotations

import asyncio
from datetime import datetime


class RuntimeControl:
    def __init__(self) -> None:
        self._paused = False
        self._reason = ""
        self._updated_at = datetime.utcnow()
        self._lock = asyncio.Lock()

    async def pause(self, reason: str = "") -> None:
        async with self._lock:
            self._paused = True
            self._reason = reason
            self._updated_at = datetime.utcnow()

    async def resume(self) -> None:
        async with self._lock:
            self._paused = False
            self._reason = ""
            self._updated_at = datetime.utcnow()

    async def status(self) -> dict[str, str | bool]:
        async with self._lock:
            return {
                "paused": self._paused,
                "reason": self._reason,
                "updated_at": self._updated_at.isoformat(),
            }

    async def is_paused(self) -> bool:
        async with self._lock:
            return self._paused
