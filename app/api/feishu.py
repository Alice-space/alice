from __future__ import annotations

import asyncio
import json
import re
from typing import Any

from fastapi import APIRouter, Depends, HTTPException, Request

from app.api.deps import get_container
from app.dependencies import AppContainer
from app.runtime.types import RuntimeTriggerEvent

router = APIRouter(prefix="/feishu", tags=["feishu"])


@router.post("/webhook")
async def feishu_webhook(
    request: Request, container: AppContainer = Depends(get_container)
) -> dict:
    body = await request.body()
    headers = {k.lower(): v for k, v in request.headers.items()}

    if not container.feishu_service.verify_signature(headers, body):
        raise HTTPException(status_code=401, detail="invalid feishu signature")

    try:
        payload = json.loads(body.decode("utf-8") or "{}")
    except json.JSONDecodeError as exc:
        raise HTTPException(status_code=400, detail="invalid json body") from exc

    payload = container.feishu_service.decrypt_if_needed(payload)

    if "challenge" in payload:
        return {"challenge": payload["challenge"]}

    event = payload.get("event", {}) if isinstance(payload, dict) else {}
    msg = event.get("message", {}) if isinstance(event, dict) else {}
    sender = event.get("sender", {}) if isinstance(event, dict) else {}

    raw_content = msg.get("content")
    text = _extract_text(raw_content)
    if not text:
        return {"ok": True, "ignored": "not text message"}

    receive_id = event.get("chat_id") or sender.get("sender_id", {}).get("open_id") or ""
    receive_id_type = "chat_id" if event.get("chat_id") else "open_id"

    normalized = text.strip().lower()
    if normalized in {"pause", "/pause"}:
        await container.runtime_control.pause("command from feishu")
        await container.feishu_service.send_message(receive_id, "Alice paused.", receive_id_type)
        return {"ok": True, "action": "paused"}

    if normalized in {"resume", "/resume"}:
        await container.runtime_control.resume()
        await container.feishu_service.send_message(receive_id, "Alice resumed.", receive_id_type)
        return {"ok": True, "action": "resumed"}

    if normalized in {"status", "/status"}:
        status = await container.runtime_control.status()
        await container.feishu_service.send_message(
            receive_id, f"Alice status: {status}", receive_id_type
        )
        return {"ok": True, "action": "status"}

    trigger = RuntimeTriggerEvent(
        trigger_type="feishu_message",
        text=text,
        metadata={
            "receive_id": receive_id,
            "receive_id_type": receive_id_type,
            "raw_event": event,
        },
    )
    asyncio.create_task(container.orchestrator.handle_trigger(trigger))
    return {"ok": True, "queued": True}


def _extract_text(raw_content: Any) -> str:
    if raw_content is None:
        return ""
    if isinstance(raw_content, str):
        try:
            parsed = json.loads(raw_content)
            text = str(parsed.get("text") or "")
        except json.JSONDecodeError:
            text = raw_content
    elif isinstance(raw_content, dict):
        text = str(raw_content.get("text") or "")
    else:
        text = str(raw_content)

    # Remove mention tags in group messages.
    text = re.sub(r"<at[^>]*>.*?</at>", "", text)
    return text.strip()
