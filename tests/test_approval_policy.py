from pathlib import Path

from app.config import Settings
from app.memory.store import FileMemoryStore
from app.services.feishu import FeishuService
from app.services.permissions import PermissionService
from app.tools.registry import ToolRegistry


def test_auto_all_policy_no_approval_for_http_get() -> None:
    settings = Settings(
        approval_mode="auto_all",
        memory_dir=Path("./memory"),
    )
    registry = ToolRegistry(
        settings=settings,
        feishu_service=FeishuService(settings),
        permission_service=PermissionService(settings),
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
    registry = ToolRegistry(
        settings=settings,
        feishu_service=FeishuService(settings),
        permission_service=PermissionService(settings),
        memory_store=FileMemoryStore(settings),
    )
    spec = registry._tools["http.request"]
    assert registry._needs_approval(spec, {"method": "POST"}) is True
