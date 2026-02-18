from pathlib import Path

from app.config import Settings
from app.memory.store import FileMemoryStore


def test_memory_merge_and_journal(tmp_path: Path) -> None:
    settings = Settings(memory_dir=tmp_path / "memory")
    store = FileMemoryStore(settings)
    store.ensure_files()

    store.merge_long_term({"Preferences": ["Likes concise responses"], "Projects": ["Alice V1"]})
    content = (tmp_path / "memory" / "MEMORY.md").read_text(encoding="utf-8")

    assert "Likes concise responses" in content
    assert "Alice V1" in content

    daily = store.append_journal_event(
        trigger_type="feishu_message",
        session_id="abc",
        input_summary="hello",
        output_summary="world",
        tool_summary="none",
    )
    assert daily.exists()
    assert "session_id: abc" in daily.read_text(encoding="utf-8")
