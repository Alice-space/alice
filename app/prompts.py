from __future__ import annotations

import json
import logging
from pathlib import Path
from typing import Any

from app.config import Settings

logger = logging.getLogger(__name__)

DEFAULT_PROMPTS: dict[str, str] = {
    "runtime.system": "You are Alice, a private assistant. Always return valid JSON only.",
    "runtime.action_instruction": "Decide one action per step: final_message or tool_call.",
    "runtime.tool_call_instruction": (
        "For tool_call include tool_name and tool_arguments_json string "
        "(JSON object serialized as string)."
    ),
    "openai.system_json": (
        "Return strict JSON with fields: "
        "action_type(final_message|tool_call), final_message, tool_name, "
        "tool_arguments_json(stringified JSON object), reasoning."
    ),
    "todo.title_summary": (
        "Summarize the following todo content into a concise actionable title. "
        "Return strict JSON with field `title` only. "
        "Keep it under 12 words and 80 characters.\\n\\n"
        "TODO content:\\n{todo_text}"
    ),
}


class PromptRegistry:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self._prompts: dict[str, str] = dict(DEFAULT_PROMPTS)
        self._load_from_file()

    @property
    def prompts_file(self) -> Path:
        return self.settings.resolved_prompts_file

    def ensure_prompts_file(self) -> None:
        path = self.prompts_file
        if path.exists():
            return
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            json.dumps(DEFAULT_PROMPTS, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )

    def get(self, key: str) -> str:
        return self._prompts.get(key, "")

    def format(self, key: str, **kwargs: Any) -> str:
        template = self.get(key)
        try:
            return template.format(**kwargs)
        except Exception:  # noqa: BLE001
            logger.warning("prompt format failed for key=%s; return raw template", key)
            return template

    def all(self) -> dict[str, str]:
        return dict(self._prompts)

    def _load_from_file(self) -> None:
        path = self.prompts_file
        if not path.exists():
            return

        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except Exception as exc:  # noqa: BLE001
            logger.warning("failed to parse prompts file %s: %s", path, exc)
            return

        if not isinstance(payload, dict):
            logger.warning("prompts file %s must be a JSON object", path)
            return

        for key, value in payload.items():
            if not isinstance(key, str) or not isinstance(value, str):
                continue
            self._prompts[key] = value
