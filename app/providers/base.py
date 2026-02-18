from __future__ import annotations

import json
from typing import Protocol

from app.runtime.types import ActionRequest, ActionResponse, ProviderHealth


class ProviderError(RuntimeError):
    pass


class ModelProvider(Protocol):
    async def health(self) -> ProviderHealth: ...

    async def plan_next_action(self, req: ActionRequest) -> ActionResponse: ...


RESPONSE_SCHEMA: dict = {
    "type": "object",
    "properties": {
        "action_type": {"type": "string", "enum": ["final_message", "tool_call"]},
        "final_message": {"type": ["string", "null"]},
        "tool_name": {"type": ["string", "null"]},
        # Strict JSON schema mode requires nested objects to set additionalProperties=false.
        # Use a JSON string for flexible tool arguments and parse it back into dict.
        "tool_arguments_json": {"type": "string"},
        "reasoning": {"type": "string"},
    },
    "required": ["action_type", "final_message", "tool_name", "tool_arguments_json", "reasoning"],
    "additionalProperties": False,
}


def parse_action_payload(payload: dict) -> ActionResponse:
    action_type = payload.get("action_type")
    if action_type not in {"final_message", "tool_call"}:
        raise ProviderError(f"invalid action_type: {action_type}")

    final_message = payload.get("final_message")
    tool_name = payload.get("tool_name")
    tool_arguments_json = payload.get("tool_arguments_json", "{}")
    if not isinstance(tool_arguments_json, str):
        raise ProviderError("tool_arguments_json must be a string")
    try:
        tool_arguments = json.loads(tool_arguments_json or "{}")
    except json.JSONDecodeError as exc:
        raise ProviderError("tool_arguments_json is not valid JSON") from exc
    if not isinstance(tool_arguments, dict):
        raise ProviderError("tool_arguments_json must decode to an object")
    reasoning = payload.get("reasoning", "")

    if action_type == "final_message" and not final_message:
        raise ProviderError("final_message action requires final_message")
    if action_type == "tool_call" and not tool_name:
        raise ProviderError("tool_call action requires tool_name")

    return ActionResponse(
        action_type=action_type,
        final_message=final_message,
        tool_name=tool_name,
        tool_arguments=tool_arguments,
        reasoning=reasoning,
    )
