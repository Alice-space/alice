from __future__ import annotations

import json
import logging

import httpx

from app.config import Settings
from app.prompts import PromptRegistry
from app.providers.base import ProviderError, parse_action_payload
from app.runtime.types import ActionRequest, ActionResponse, ProviderHealth

logger = logging.getLogger(__name__)


class OpenAIAPIProvider:
    def __init__(self, settings: Settings, prompt_registry: PromptRegistry) -> None:
        self.settings = settings
        self.prompt_registry = prompt_registry

    async def health(self) -> ProviderHealth:
        if not self.settings.openai_api_key:
            return ProviderHealth(provider="openai_api", ok=False, detail="missing API key")
        return ProviderHealth(provider="openai_api", ok=True, detail="ok")

    async def plan_next_action(self, req: ActionRequest) -> ActionResponse:
        if not self.settings.openai_api_key:
            raise ProviderError("openai api key is not configured")

        system_prompt = self.prompt_registry.get("openai.system_json")

        payload = {
            "model": self.settings.openai_model,
            "response_format": {"type": "json_object"},
            "temperature": 0,
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": req.prompt},
            ],
        }

        headers = {
            "Authorization": f"Bearer {self.settings.openai_api_key}",
            "Content-Type": "application/json",
        }

        async with httpx.AsyncClient(timeout=self.settings.openai_timeout_seconds) as client:
            response = await client.post(
                "https://api.openai.com/v1/chat/completions",
                headers=headers,
                json=payload,
            )

        if response.status_code >= 400:
            raise ProviderError(
                f"openai request failed: {response.status_code} {response.text[:200]}"
            )

        try:
            content = response.json()["choices"][0]["message"]["content"]
            parsed = json.loads(content)
        except Exception as exc:  # noqa: BLE001
            raise ProviderError("invalid openai response payload") from exc

        action = parse_action_payload(parsed)
        action.provider_used = "openai_api"
        logger.info("openai action=%s", action.action_type)
        return action
