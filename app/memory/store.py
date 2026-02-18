from __future__ import annotations

import os
import tempfile
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

from filelock import FileLock

from app.config import Settings

SECTIONS = [
    "People",
    "Projects",
    "Preferences",
    "Operational Rules",
    "Open Questions",
]


class FileMemoryStore:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.memory_dir: Path = settings.resolved_memory_dir
        self.memory_file: Path = self.memory_dir / "MEMORY.md"
        self.lock_file: Path = self.memory_dir / ".memory.lock"

    def ensure_files(self) -> None:
        self.memory_dir.mkdir(parents=True, exist_ok=True)
        if not self.memory_file.exists():
            self._atomic_write(self.memory_file, self._render_sections({}))

    def read_long_term_summary(self, max_chars: int = 4000) -> str:
        self.ensure_files()
        content = self.memory_file.read_text(encoding="utf-8")
        return content[:max_chars]

    def read_daily_tail(self, max_chars: int = 2000) -> str:
        daily_file = self._daily_path()
        if not daily_file.exists():
            return ""
        text = daily_file.read_text(encoding="utf-8")
        return text[-max_chars:]

    def append_journal_event(
        self,
        trigger_type: str,
        session_id: str,
        input_summary: str,
        output_summary: str,
        tool_summary: str,
    ) -> Path:
        self.ensure_files()
        lock = FileLock(str(self.lock_file))
        with lock:
            daily_file = self._daily_path()
            now = self._now()
            if not daily_file.exists():
                daily_file.write_text(f"# {daily_file.stem}\n\n", encoding="utf-8")
            with daily_file.open("a", encoding="utf-8") as handle:
                handle.write(f"## {now.strftime('%H:%M:%S')} | {trigger_type}\n")
                handle.write(f"- session_id: {session_id}\n")
                handle.write(f"- input: {input_summary.strip()[:600]}\n")
                handle.write(f"- tools: {tool_summary.strip()[:600]}\n")
                handle.write(f"- output: {output_summary.strip()[:1200]}\n\n")
        return daily_file

    def merge_long_term(self, updates: dict[str, list[str]]) -> None:
        self.ensure_files()
        lock = FileLock(str(self.lock_file))
        with lock:
            sections = self._parse_sections(self.memory_file.read_text(encoding="utf-8"))
            for section, entries in updates.items():
                if section not in SECTIONS:
                    continue
                current = sections.setdefault(section, [])
                seen = set(current)
                for entry in entries:
                    normalized = entry.strip()
                    if not normalized or normalized in seen:
                        continue
                    current.append(normalized)
                    seen.add(normalized)

            body = self._render_sections(sections)
            self._atomic_write(self.memory_file, body)

    def _now(self) -> datetime:
        if self.settings.timezone:
            return datetime.now(ZoneInfo(self.settings.timezone))
        return datetime.now().astimezone()

    def _daily_path(self) -> Path:
        return self.memory_dir / f"{self._now().strftime('%Y-%m-%d')}.md"

    def _render_sections(self, sections: dict[str, list[str]]) -> str:
        lines = ["# MEMORY", ""]
        for section in SECTIONS:
            lines.append(f"## {section}")
            entries = sections.get(section, [])
            if entries:
                lines.extend([f"- {entry}" for entry in entries])
            else:
                lines.append("- (empty)")
            lines.append("")
        return "\n".join(lines).rstrip() + "\n"

    def _parse_sections(self, content: str) -> dict[str, list[str]]:
        parsed: dict[str, list[str]] = {section: [] for section in SECTIONS}
        current: str | None = None
        for raw_line in content.splitlines():
            line = raw_line.strip()
            if line.startswith("## "):
                section = line[3:].strip()
                current = section if section in parsed else None
                continue
            if current is None:
                continue
            if line.startswith("- "):
                value = line[2:].strip()
                if value and value != "(empty)":
                    parsed[current].append(value)
        return parsed

    def _atomic_write(self, target: Path, content: str) -> None:
        fd, temp_path = tempfile.mkstemp(prefix=f".{target.name}.", dir=str(target.parent))
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                handle.write(content)
            os.replace(temp_path, target)
        finally:
            try:
                os.unlink(temp_path)
            except OSError:
                pass
