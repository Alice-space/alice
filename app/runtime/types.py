from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, Field

TriggerType = Literal["feishu_message", "schedule_fire", "todo_non_empty"]
ActionType = Literal["final_message", "tool_call"]


class RuntimeTriggerEvent(BaseModel):
    trigger_type: TriggerType
    text: str = ""
    metadata: dict[str, Any] = Field(default_factory=dict)


class ProviderHealth(BaseModel):
    provider: str
    ok: bool
    detail: str = ""


class ActionRequest(BaseModel):
    session_id: str
    step_index: int
    prompt: str
    context: dict[str, Any] = Field(default_factory=dict)


class ActionResponse(BaseModel):
    action_type: ActionType
    final_message: str | None = None
    tool_name: str | None = None
    tool_arguments: dict[str, Any] = Field(default_factory=dict)
    reasoning: str = ""
    provider_used: str = ""
    fallback_reason: str | None = None


class ToolExecutionResult(BaseModel):
    ok: bool
    output: dict[str, Any] = Field(default_factory=dict)
    error: str | None = None
    requires_approval: bool = False
    permission_request_id: str | None = None


class StreamEvent(BaseModel):
    session_id: str
    event_type: str
    payload: dict[str, Any] = Field(default_factory=dict)
    ts: datetime = Field(default_factory=datetime.utcnow)
