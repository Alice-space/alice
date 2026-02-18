from __future__ import annotations

import asyncio
import json
import logging
import os
import tempfile
from typing import Any

from app.config import Settings
from app.providers.base import RESPONSE_SCHEMA, ProviderError, parse_action_payload
from app.runtime.types import ActionRequest, ActionResponse, ProviderHealth

logger = logging.getLogger(__name__)


def parse_codex_jsonl_output(output: str) -> tuple[list[dict[str, Any]], str | None]:
    events: list[dict[str, Any]] = []
    last_agent_message: str | None = None

    for raw_line in output.splitlines():
        line = raw_line.strip()
        if not line:
            continue
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue

        if not isinstance(item, dict):
            continue

        events.append(item)
        if item.get("type") == "item.completed":
            payload = item.get("item", {})
            if isinstance(payload, dict) and payload.get("type") == "agent_message":
                text = payload.get("text")
                if isinstance(text, str):
                    last_agent_message = text

    return events, last_agent_message


class CodexExecProvider:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings

    async def health(self) -> ProviderHealth:
        cmd = [self.settings.codex_command, "login", "status"]
        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=10)
        except FileNotFoundError:
            return ProviderHealth(provider="codex_exec", ok=False, detail="codex binary not found")
        except TimeoutError:
            return ProviderHealth(
                provider="codex_exec", ok=False, detail="codex login status timeout"
            )

        output = (stdout + stderr).decode("utf-8", errors="replace")
        ok = proc.returncode == 0 and "Logged in using ChatGPT" in output
        detail = "ok" if ok else output.strip()[:200]
        return ProviderHealth(provider="codex_exec", ok=ok, detail=detail)

    async def plan_next_action(self, req: ActionRequest) -> ActionResponse:
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
        cmd.append(req.prompt)

        logger.info("codex exec step=%s session=%s", req.step_index, req.session_id)
        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env={**os.environ},
            )
            stdout, stderr = await asyncio.wait_for(
                proc.communicate(),
                timeout=self.settings.codex_timeout_seconds,
            )
        except FileNotFoundError as exc:
            raise ProviderError("codex binary not found") from exc
        except TimeoutError as exc:
            raise ProviderError("codex exec timeout") from exc
        finally:
            try:
                os.unlink(schema_path)
            except OSError:
                pass

        output = stdout.decode("utf-8", errors="replace")
        stderr_text = stderr.decode("utf-8", errors="replace")

        if proc.returncode != 0:
            detail = _extract_codex_error_detail(output, stderr_text)
            raise ProviderError(f"codex exec failed (exit={proc.returncode}): {detail}")

        _, final_text = parse_codex_jsonl_output(output)
        if not final_text:
            raise ProviderError("codex exec returned no final agent_message")

        try:
            payload = json.loads(final_text)
        except json.JSONDecodeError as exc:
            raise ProviderError(f"codex final message is not JSON: {final_text[:200]}") from exc

        action = parse_action_payload(payload)
        action.provider_used = "codex_exec"
        return action

    def _write_schema_file(self) -> str:
        fd, path = tempfile.mkstemp(prefix="alice-action-schema-", suffix=".json")
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(RESPONSE_SCHEMA, handle)
        return path


def _extract_codex_error_detail(stdout_text: str, stderr_text: str) -> str:
    events, _ = parse_codex_jsonl_output(stdout_text)
    for event in reversed(events):
        if event.get("type") == "error":
            message = event.get("message")
            if isinstance(message, str) and message.strip():
                return message.strip()[:500]
        if event.get("type") == "turn.failed":
            err = event.get("error")
            if isinstance(err, dict):
                msg = err.get("message")
                if isinstance(msg, str) and msg.strip():
                    return msg.strip()[:500]

    merged = (stderr_text or "").strip()
    if merged:
        return merged[:500]

    compact_stdout = (stdout_text or "").strip().replace("\n", " ")
    return compact_stdout[:500] or "no output"
