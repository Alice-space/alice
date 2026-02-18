from __future__ import annotations

import asyncio
import json
from collections import defaultdict
from collections.abc import AsyncIterator
from typing import Any


class SessionStreamHub:
    def __init__(self) -> None:
        self._queues: dict[str, list[asyncio.Queue[str]]] = defaultdict(list)
        self._lock = asyncio.Lock()

    async def subscribe(self, session_id: str) -> asyncio.Queue[str]:
        queue: asyncio.Queue[str] = asyncio.Queue()
        async with self._lock:
            self._queues[session_id].append(queue)
        return queue

    async def unsubscribe(self, session_id: str, queue: asyncio.Queue[str]) -> None:
        async with self._lock:
            queues = self._queues.get(session_id, [])
            if queue in queues:
                queues.remove(queue)
            if not queues and session_id in self._queues:
                del self._queues[session_id]

    async def publish(self, session_id: str, event_type: str, payload: dict[str, Any]) -> None:
        async with self._lock:
            queues = list(self._queues.get(session_id, []))
        if not queues:
            return
        body = json.dumps({"event_type": event_type, "payload": payload})
        for queue in queues:
            queue.put_nowait(body)

    async def sse_generator(self, session_id: str) -> AsyncIterator[str]:
        queue = await self.subscribe(session_id)
        try:
            while True:
                try:
                    item = await asyncio.wait_for(queue.get(), timeout=15)
                    yield f"data: {item}\n\n"
                except TimeoutError:
                    yield ": keepalive\n\n"
        finally:
            await self.unsubscribe(session_id, queue)
