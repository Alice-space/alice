from pathlib import Path

from app.config import Settings
from app.memory.store import FileMemoryStore
from app.prompts import PromptRegistry
from app.services.feishu import FeishuService
from app.services.permissions import PermissionService
from app.services.todo_title import TodoTitleGenerator
from app.tools.registry import ToolRegistry


def test_auto_all_policy_no_approval_for_http_get() -> None:
    settings = Settings(
        approval_mode="auto_all",
        memory_dir=Path("./memory"),
    )
    prompt_registry = PromptRegistry(settings)
    registry = ToolRegistry(
        settings=settings,
        feishu_service=FeishuService(settings),
        permission_service=PermissionService(settings),
        todo_title_generator=TodoTitleGenerator(settings, prompt_registry),
        memory_store=FileMemoryStore(settings),
    )
    spec = registry._tools["http.request"]
    assert registry._needs_approval(spec, {"method": "GET"}) is False


def test_http_write_can_require_approval() -> None:
    settings = Settings(
        approval_mode="auto_all",
        http_write_requires_approval=True,
        memory_dir=Path("./memory"),
    )
    prompt_registry = PromptRegistry(settings)
    registry = ToolRegistry(
        settings=settings,
        feishu_service=FeishuService(settings),
        permission_service=PermissionService(settings),
        todo_title_generator=TodoTitleGenerator(settings, prompt_registry),
        memory_store=FileMemoryStore(settings),
    )
    spec = registry._tools["http.request"]
    assert registry._needs_approval(spec, {"method": "POST"}) is True
