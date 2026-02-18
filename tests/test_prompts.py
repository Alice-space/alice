from pathlib import Path

from app.config import Settings
from app.prompts import DEFAULT_PROMPTS, PromptRegistry


def test_prompt_registry_uses_defaults_when_file_missing(tmp_path: Path) -> None:
    settings = Settings(state_dir=tmp_path)
    registry = PromptRegistry(settings)

    assert registry.get("runtime.system") == DEFAULT_PROMPTS["runtime.system"]
    assert registry.get("todo.title_summary")


def test_prompt_registry_loads_override_file(tmp_path: Path) -> None:
    prompts_file = tmp_path / "custom-prompts.json"
    prompts_file.write_text(
        '{"runtime.system":"You are Alice Custom.","openai.system_json":"Only JSON please."}',
        encoding="utf-8",
    )

    settings = Settings(state_dir=tmp_path, prompts_file=prompts_file)
    registry = PromptRegistry(settings)

    assert registry.get("runtime.system") == "You are Alice Custom."
    assert registry.get("openai.system_json") == "Only JSON please."


def test_prompt_registry_writes_default_file(tmp_path: Path) -> None:
    settings = Settings(state_dir=tmp_path)
    registry = PromptRegistry(settings)
    registry.ensure_prompts_file()

    path = settings.resolved_prompts_file
    assert path.exists()
    content = path.read_text(encoding="utf-8")
    assert "runtime.system" in content
