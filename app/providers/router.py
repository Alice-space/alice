from __future__ import annotations

import logging

from app.config import Settings
from app.prompts import PromptRegistry
from app.providers.base import ModelProvider, ProviderError
from app.providers.codex_exec import CodexExecProvider
from app.providers.openai_api import OpenAIAPIProvider
from app.runtime.types import ActionRequest, ActionResponse, ProviderHealth

logger = logging.getLogger(__name__)


class ModelRouter:
    def __init__(self, settings: Settings, prompt_registry: PromptRegistry) -> None:
        self.settings = settings
        self.codex = CodexExecProvider(settings)
        self.openai = OpenAIAPIProvider(settings, prompt_registry)

    async def health(self) -> list[ProviderHealth]:
        return [await self.codex.health(), await self.openai.health()]

    async def codex_login_status(self) -> ProviderHealth:
        return await self.codex.health()

    async def plan_next_action(self, req: ActionRequest) -> ActionResponse:
        primary: ModelProvider
        fallback: ModelProvider
        if self.settings.default_provider == "openai_api":
            primary = self.openai
            fallback = self.codex
            fallback_name = "codex_exec"
        else:
            primary = self.codex
            fallback = self.openai
            fallback_name = "openai_api"

        try:
            return await primary.plan_next_action(req)
        except ProviderError as primary_error:
            logger.warning("primary provider failed: %s", primary_error)
            try:
                response = await fallback.plan_next_action(req)
                response.fallback_reason = str(primary_error)
                return response
            except ProviderError as fallback_error:
                raise ProviderError(
                    f"both providers failed; primary={primary_error}; {fallback_name}={fallback_error}"
                ) from fallback_error
