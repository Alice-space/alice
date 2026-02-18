from __future__ import annotations

import logging
from datetime import datetime
from typing import Any

from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.config import Settings
from app.db.models import (
    MessageRecord,
    RuntimeEventRecord,
    SessionRecord,
    SessionStatus,
    ToolInvocationRecord,
    TurnRecord,
)
from app.memory.store import FileMemoryStore
from app.prompts import PromptRegistry
from app.providers.base import ProviderError
from app.providers.router import ModelRouter
from app.runtime.context import ContextAssembler
from app.runtime.control import RuntimeControl
from app.runtime.stream import SessionStreamHub
from app.runtime.types import ActionRequest, RuntimeTriggerEvent
from app.services.feishu import FeishuService
from app.tools.registry import ToolRegistry

logger = logging.getLogger(__name__)


class RuntimeOrchestrator:
    def __init__(
        self,
        settings: Settings,
        session_factory: async_sessionmaker[AsyncSession],
        model_router: ModelRouter,
        tool_registry: ToolRegistry,
        prompt_registry: PromptRegistry,
        memory_store: FileMemoryStore,
        stream_hub: SessionStreamHub,
        runtime_control: RuntimeControl,
        feishu_service: FeishuService,
    ) -> None:
        self.settings = settings
        self.session_factory = session_factory
        self.model_router = model_router
        self.tool_registry = tool_registry
        self.prompt_registry = prompt_registry
        self.memory_store = memory_store
        self.stream_hub = stream_hub
        self.runtime_control = runtime_control
        self.feishu_service = feishu_service
        self.context_assembler = ContextAssembler(
            settings,
            memory_store,
            tool_registry,
            prompt_registry,
        )

    async def handle_trigger(self, event: RuntimeTriggerEvent) -> str | None:
        if await self.runtime_control.is_paused():
            await self._reply_paused_if_needed(event)
            return None

        async with self.session_factory() as db:
            session = SessionRecord(trigger_source=event.trigger_type, metadata_json=event.metadata)
            db.add(session)
            await db.flush()
            session_id = session.id

            await self._record_event(
                db, session_id, "session.started", {"trigger": event.trigger_type}
            )
            if event.text:
                db.add(
                    MessageRecord(
                        session_id=session_id,
                        role="user",
                        content=event.text,
                        metadata_json=event.metadata,
                    )
                )
            await db.commit()

        await self.stream_hub.publish(
            session_id, "session.started", {"trigger": event.trigger_type}
        )

        tool_traces: list[dict[str, Any]] = []
        final_message = ""
        final_provider = ""
        fallback_reason: str | None = None

        try:
            for step_index in range(1, self.settings.runtime_max_steps + 1):
                async with self.session_factory() as db:
                    context = await self.context_assembler.build(
                        db,
                        session_id=session_id,
                        event=event,
                        tool_results=tool_traces,
                    )
                    req = ActionRequest(
                        session_id=session_id,
                        step_index=step_index,
                        prompt=context["prompt"],
                        context={"event": event.model_dump(), "tools": context["tool_catalog"]},
                    )

                try:
                    action = await self.model_router.plan_next_action(req)
                except ProviderError as exc:
                    await self._finish_failed(session_id, str(exc))
                    await self.stream_hub.publish(session_id, "session.failed", {"error": str(exc)})
                    return session_id

                final_provider = action.provider_used
                if action.fallback_reason:
                    fallback_reason = action.fallback_reason

                async with self.session_factory() as db:
                    db.add(
                        TurnRecord(
                            session_id=session_id,
                            step_index=step_index,
                            provider_used=action.provider_used,
                            action_type=action.action_type,
                            request_text=req.prompt[-4000:],
                            response_text=action.model_dump_json(),
                        )
                    )
                    await db.flush()

                    if action.action_type == "final_message":
                        final_message = action.final_message or ""
                        db.add(
                            MessageRecord(
                                session_id=session_id, role="assistant", content=final_message
                            )
                        )
                        await self._record_event(
                            db,
                            session_id,
                            "assistant.final_message",
                            {"message": final_message, "provider": action.provider_used},
                        )
                        await db.commit()
                        break

                    tool_name = action.tool_name or ""
                    result = await self.tool_registry.execute(
                        db,
                        session_id=session_id,
                        tool_name=tool_name,
                        arguments=action.tool_arguments,
                    )
                    db.add(
                        ToolInvocationRecord(
                            session_id=session_id,
                            tool_name=tool_name,
                            arguments_json=action.tool_arguments,
                            result_json=result.output,
                            status="ok" if result.ok else "error",
                            error=result.error or "",
                            retry_count=0,
                        )
                    )

                    tool_trace = {
                        "tool": tool_name,
                        "arguments": action.tool_arguments,
                        "ok": result.ok,
                        "output": result.output,
                        "error": result.error,
                    }
                    tool_traces.append(tool_trace)
                    db.add(
                        MessageRecord(
                            session_id=session_id,
                            role="tool",
                            content=str(tool_trace),
                            metadata_json={"tool": tool_name},
                        )
                    )

                    await self._record_event(db, session_id, "tool.executed", tool_trace)
                    await db.commit()

                    await self.stream_hub.publish(
                        session_id,
                        "tool.executed",
                        {
                            "tool": tool_name,
                            "ok": result.ok,
                            "requires_approval": result.requires_approval,
                            "permission_request_id": result.permission_request_id,
                        },
                    )

                    if result.requires_approval:
                        final_message = (
                            f"Tool {tool_name} requires approval: {result.permission_request_id}. "
                            "Use the permission API to approve or reject."
                        )
                        db.add(
                            MessageRecord(
                                session_id=session_id, role="assistant", content=final_message
                            )
                        )
                        await db.commit()
                        break

                    if not result.ok:
                        final_message = f"Tool {tool_name} failed: {result.error}"
                        db.add(
                            MessageRecord(
                                session_id=session_id, role="assistant", content=final_message
                            )
                        )
                        await db.commit()
                        break
            else:
                final_message = "Stopped after max reasoning steps without final answer."

            if not final_message:
                final_message = "Completed without a final message."

            await self._finish_success(
                session_id=session_id,
                final_message=final_message,
                provider_used=final_provider,
                fallback_reason=fallback_reason,
                trigger_type=event.trigger_type,
                input_text=event.text,
                tool_traces=tool_traces,
            )

            await self.stream_hub.publish(
                session_id,
                "session.completed",
                {"final_message": final_message, "provider": final_provider},
            )
            await self._reply_feishu_if_needed(event, final_message)
            return session_id

        except Exception as exc:  # noqa: BLE001
            logger.exception("runtime crash")
            await self._finish_failed(session_id, str(exc))
            await self.stream_hub.publish(session_id, "session.failed", {"error": str(exc)})
            return session_id

    async def _finish_success(
        self,
        *,
        session_id: str,
        final_message: str,
        provider_used: str,
        fallback_reason: str | None,
        trigger_type: str,
        input_text: str,
        tool_traces: list[dict[str, Any]],
    ) -> None:
        async with self.session_factory() as db:
            record = await db.get(SessionRecord, session_id)
            if record:
                record.status = SessionStatus.completed.value
                record.provider_used = provider_used
                record.fallback_reason = fallback_reason
                record.updated_at = datetime.utcnow()
            await self._record_event(
                db, session_id, "session.completed", {"provider": provider_used}
            )
            await db.commit()

        tool_summary = "; ".join(
            [f"{t['tool']}:{'ok' if t['ok'] else 'fail'}" for t in tool_traces]
        )
        self.memory_store.append_journal_event(
            trigger_type=trigger_type,
            session_id=session_id,
            input_summary=input_text,
            output_summary=final_message,
            tool_summary=tool_summary or "none",
        )
        self.memory_store.merge_long_term(self._derive_memory_updates(input_text, final_message))

    async def _finish_failed(self, session_id: str, error: str) -> None:
        async with self.session_factory() as db:
            record = await db.get(SessionRecord, session_id)
            if record:
                record.status = SessionStatus.failed.value
                record.updated_at = datetime.utcnow()
                record.fallback_reason = error
            await self._record_event(db, session_id, "session.failed", {"error": error})
            await db.commit()

    async def _record_event(
        self,
        db: AsyncSession,
        session_id: str | None,
        event_type: str,
        payload: dict[str, Any],
    ) -> None:
        db.add(
            RuntimeEventRecord(session_id=session_id, event_type=event_type, payload_json=payload)
        )

    def _derive_memory_updates(self, user_text: str, assistant_text: str) -> dict[str, list[str]]:
        updates: dict[str, list[str]] = {
            "Operational Rules": [
                f"Approval mode is {self.settings.approval_mode}.",
                "Codex provider is primary and OpenAI API is fallback.",
            ]
        }

        lowered = (user_text or "").lower()
        if any(word in lowered for word in ["prefer", "偏好", "喜欢", "习惯"]):
            updates.setdefault("Preferences", []).append(user_text[:200])
        if any(word in lowered for word in ["project", "项目", "alice"]):
            updates.setdefault("Projects", []).append(user_text[:200])

        if assistant_text:
            updates.setdefault("Open Questions", []).append(
                f"Last output summary: {assistant_text[:200]}"
            )
        return updates

    async def _reply_paused_if_needed(self, event: RuntimeTriggerEvent) -> None:
        if event.trigger_type != "feishu_message":
            return
        receive_id = str(event.metadata.get("receive_id") or "")
        if not receive_id:
            return
        receive_id_type = str(event.metadata.get("receive_id_type") or "chat_id")
        await self.feishu_service.send_message(receive_id, "Alice is paused.", receive_id_type)

    async def _reply_feishu_if_needed(self, event: RuntimeTriggerEvent, text: str) -> None:
        if event.trigger_type != "feishu_message":
            return
        receive_id = str(event.metadata.get("receive_id") or "")
        if not receive_id:
            return
        receive_id_type = str(event.metadata.get("receive_id_type") or "chat_id")
        await self.feishu_service.send_message(
            receive_id=receive_id, text=text, receive_id_type=receive_id_type
        )
