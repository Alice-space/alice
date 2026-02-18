from __future__ import annotations

import asyncio
import json
import logging
import os
import re
import tempfile
from typing import Any

from app.config import Settings
from app.prompts import PromptRegistry
from app.providers.codex_exec import parse_codex_jsonl_output

logger = logging.getLogger(__name__)

_TITLE_SCHEMA: dict[str, Any] = {
    "type": "object",
    "properties": {
        "title": {"type": "string"},
    },
    "required": ["title"],
    "additionalProperties": False,
}


class TodoTitleGenerator:
    def __init__(self, settings: Settings, prompt_registry: PromptRegistry) -> None:
        self.settings = settings
        self.prompt_registry = prompt_registry

    async def summarize(self, text: str) -> tuple[str, str]:
        raw = self._normalize_input(text)
        if not raw:
            return "Untitled Todo", "fallback"

        title = await self._summarize_with_codex(raw)
        if title:
            return title, "codex"

        return self._fallback_title(raw), "fallback"

    async def _summarize_with_codex(self, text: str) -> str | None:
        schema_path = self._write_schema_file()
        cmd = [
            self.settings.codex_command,
            "exec",
            "--json",
            "--skip-git-repo-check",
            "--sandbox",
            "read-only",
            "--output-schema",
            schema_path,
            "-C",
            str(self.settings.resolved_workspace_dir),
        ]
        if self.settings.codex_model:
            cmd.extend(["--model", self.settings.codex_model])

        prompt = self.prompt_registry.format("todo.title_summary", todo_text=text)
        cmd.append(prompt)

        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env={**os.environ},
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=45)
        except FileNotFoundError:
            logger.warning("codex command not found, fallback todo title")
            return None
        except TimeoutError:
            logger.warning("codex title summarization timeout")
            return None
        finally:
            try:
                os.unlink(schema_path)
            except OSError:
                pass

        output = stdout.decode("utf-8", errors="replace")
        stderr_text = stderr.decode("utf-8", errors="replace")
        if proc.returncode != 0:
            logger.warning("codex title summarization failed: %s", (stderr_text or output)[:240])
            return None

        _, final_text = parse_codex_jsonl_output(output)
        if not final_text:
            return None

        try:
            payload = json.loads(final_text)
            title = str(payload.get("title") or "")
        except Exception:  # noqa: BLE001
            return None

        return self._sanitize_title(title)

    def _fallback_title(self, text: str) -> str:
        sentence = re.split(r"[\n。！？!?\.]+", text, maxsplit=1)[0].strip()
        if not sentence:
            sentence = text.strip()

        words = sentence.split()
        if len(words) > 12:
            sentence = " ".join(words[:12])
        return self._sanitize_title(sentence) or "Untitled Todo"

    @staticmethod
    def _normalize_input(text: str) -> str:
        return re.sub(r"\s+", " ", text).strip()

    @staticmethod
    def _sanitize_title(title: str) -> str:
        cleaned = re.sub(r"\s+", " ", (title or "").strip().strip('"').strip("'"))
        if len(cleaned) > 80:
            cleaned = cleaned[:80].rstrip()
        return cleaned

    @staticmethod
    def _write_schema_file() -> str:
        fd, path = tempfile.mkstemp(prefix="alice-title-schema-", suffix=".json")
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(_TITLE_SCHEMA, handle)
        return path
