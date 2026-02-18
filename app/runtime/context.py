from __future__ import annotations

import re
from pathlib import Path
from typing import Any

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.config import Settings
from app.db.models import MessageRecord
from app.memory.store import FileMemoryStore
from app.prompts import PromptRegistry
from app.runtime.types import RuntimeTriggerEvent
from app.tools.registry import ToolRegistry


class ContextAssembler:
    def __init__(
        self,
        settings: Settings,
        memory_store: FileMemoryStore,
        tool_registry: ToolRegistry,
        prompt_registry: PromptRegistry,
    ) -> None:
        self.settings = settings
        self.memory_store = memory_store
        self.tool_registry = tool_registry
        self.prompt_registry = prompt_registry

    async def build(
        self,
        db: AsyncSession,
        *,
        session_id: str,
        event: RuntimeTriggerEvent,
        tool_results: list[dict[str, Any]],
    ) -> dict[str, Any]:
        recent_messages = await self._load_recent_messages(db, session_id)
        memory_summary = self.memory_store.read_long_term_summary()
        journal_tail = self.memory_store.read_daily_tail()
        skills = self._load_matching_skills(event.text)

        prompt = self._render_prompt(
            event=event,
            recent_messages=recent_messages,
            memory_summary=memory_summary,
            journal_tail=journal_tail,
            skills=skills,
            tool_results=tool_results,
        )

        return {
            "prompt": prompt,
            "recent_messages": recent_messages,
            "memory_summary": memory_summary,
            "journal_tail": journal_tail,
            "skills": skills,
            "tool_catalog": self.tool_registry.describe_tools(),
        }

    async def _load_recent_messages(
        self, db: AsyncSession, session_id: str
    ) -> list[dict[str, str]]:
        rows = await db.execute(
            select(MessageRecord)
            .where(MessageRecord.session_id == session_id)
            .order_by(MessageRecord.id.desc())
            .limit(self.settings.session_context_window)
        )
        records = list(reversed(rows.scalars().all()))
        return [{"role": row.role, "content": row.content} for row in records]

    def _load_matching_skills(self, query: str) -> list[dict[str, str]]:
        skill_dir = self.settings.resolved_skills_dir
        if not skill_dir.exists():
            return []

        tokens = {token.lower() for token in re.findall(r"[\w\-]{3,}", query or "")}
        matches: list[dict[str, str]] = []

        for file_path in sorted(skill_dir.glob("*.md"))[:50]:
            try:
                content = file_path.read_text(encoding="utf-8")
            except OSError:
                continue

            name = file_path.stem.lower()
            score = 0
            if name in tokens:
                score += 2
            lowered = content.lower()
            for token in tokens:
                if token in lowered:
                    score += 1
            if score <= 0:
                continue
            matches.append(
                {
                    "name": file_path.stem,
                    "path": str(Path(file_path).resolve()),
                    "preview": content[:300],
                    "score": str(score),
                }
            )

        matches.sort(key=lambda item: int(item["score"]), reverse=True)
        return matches[:5]

    def _render_prompt(
        self,
        *,
        event: RuntimeTriggerEvent,
        recent_messages: list[dict[str, str]],
        memory_summary: str,
        journal_tail: str,
        skills: list[dict[str, str]],
        tool_results: list[dict[str, Any]],
    ) -> str:
        lines: list[str] = []
        lines.append(self.prompt_registry.get("runtime.system"))
        lines.append(self.prompt_registry.get("runtime.action_instruction"))
        lines.append(self.prompt_registry.get("runtime.tool_call_instruction"))
        lines.append("")
        lines.append(f"Trigger: {event.trigger_type}")
        lines.append(f"User input: {event.text}")
        lines.append("")
        lines.append("Available tools:")
        for tool in self.tool_registry.describe_tools():
            lines.append(f"- {tool['name']} (trusted={tool['trusted']})")
        lines.append("")

        lines.append("Recent session messages:")
        if recent_messages:
            for msg in recent_messages[-20:]:
                lines.append(f"[{msg['role']}] {msg['content']}")
        else:
            lines.append("(none)")
        lines.append("")

        lines.append("Tool results so far:")
        if tool_results:
            for item in tool_results[-8:]:
                lines.append(f"- {item}")
        else:
            lines.append("(none)")
        lines.append("")

        lines.append("Matched skills:")
        if skills:
            for skill in skills:
                lines.append(f"- {skill['name']}: {skill['preview']}")
        else:
            lines.append("(none)")
        lines.append("")

        lines.append("Long-term memory summary:")
        lines.append(memory_summary or "(empty)")
        lines.append("")

        lines.append("Today's journal tail:")
        lines.append(journal_tail or "(empty)")

        return "\n".join(lines)
