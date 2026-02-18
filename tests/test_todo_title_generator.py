from pathlib import Path

from app.config import Settings
from app.prompts import PromptRegistry
from app.services.todo_title import TodoTitleGenerator


async def test_title_generator_prefers_codex(monkeypatch) -> None:
    settings = Settings(state_dir=Path("/tmp/alice-test-state"))
    prompt_registry = PromptRegistry(settings)
    generator = TodoTitleGenerator(settings, prompt_registry)

    async def fake_codex(_: str) -> str | None:
        return "Write weekly project report"

    monkeypatch.setattr(generator, "_summarize_with_codex", fake_codex)
    title, source = await generator.summarize("Need to finish and send this week report")

    assert title == "Write weekly project report"
    assert source == "codex"


async def test_title_generator_fallback_when_codex_fails(monkeypatch) -> None:
    settings = Settings(state_dir=Path("/tmp/alice-test-state"))
    prompt_registry = PromptRegistry(settings)
    generator = TodoTitleGenerator(settings, prompt_registry)

    async def fake_codex(_: str) -> str | None:
        return None

    monkeypatch.setattr(generator, "_summarize_with_codex", fake_codex)
    title, source = await generator.summarize(
        "Prepare slides for investor update tomorrow morning and send to team"
    )

    assert title
    assert len(title) <= 80
    assert source == "fallback"
