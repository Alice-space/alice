from __future__ import annotations

from functools import lru_cache
from pathlib import Path
from typing import Literal

from pydantic import AliasChoices, Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="ALICE_",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        case_sensitive=False,
    )

    app_name: str = "Alice"
    environment: str = "dev"
    host: str = "0.0.0.0"
    port: int = 8000

    api_prefix: str = "/api/v1"
    api_token: str = "dev-token"

    # Runtime state is decoupled from project code:
    # db, memory, and skills default under this directory.
    state_dir: Path = Field(default_factory=lambda: Path.home() / ".alice")
    prompts_file: Path | None = None
    database_url: str | None = None

    timezone: str | None = None
    runtime_max_steps: int = 8
    session_context_window: int = 20

    memory_dir: Path | None = None
    skills_dir: Path | None = None
    workspace_dir: Path = Path(".")

    codex_command: str = "codex"
    codex_model: str | None = None
    codex_timeout_seconds: int = 180

    openai_api_key: str | None = Field(
        default=None,
        validation_alias=AliasChoices("ALICE_OPENAI_API_KEY", "OPENAI_API_KEY"),
    )
    openai_model: str = "gpt-5-mini"
    openai_timeout_seconds: int = 90

    default_provider: Literal["codex_exec", "openai_api"] = "codex_exec"

    # Approval strategy updated for standalone deployment: auto by default.
    approval_mode: Literal["auto_all", "trusted_only", "explicit_only"] = "auto_all"
    approval_required_tools: list[str] = Field(default_factory=list)
    permission_timeout_seconds: int = 1800
    tool_retry_max_attempts: int = 3
    http_write_requires_approval: bool = False

    trusted_tools: list[str] = Field(
        default_factory=lambda: [
            "feishu.send_message",
            "todo.create",
            "todo.update",
            "todo.list",
            "http.request",
        ]
    )

    feishu_base_url: str = "https://open.feishu.cn"
    feishu_app_id: str | None = None
    feishu_app_secret: str | None = None
    feishu_bot_token: str | None = None
    feishu_signing_secret: str | None = None
    feishu_encrypt_key: str | None = None

    scheduler_enabled: bool = True

    @property
    def resolved_state_dir(self) -> Path:
        return self._resolve_path(self.state_dir, base=Path.cwd())

    @property
    def resolved_database_url(self) -> str:
        if self.database_url:
            if self.database_url.startswith("sqlite+aiosqlite:///"):
                raw_path = self.database_url.replace("sqlite+aiosqlite:///", "", 1)
                db_path = Path(raw_path)
                if not db_path.is_absolute():
                    db_path = self.resolved_state_dir / db_path
                return f"sqlite+aiosqlite:///{db_path.resolve()}"
            return self.database_url

        db_path = (self.resolved_state_dir / "db" / "alice.db").resolve()
        return f"sqlite+aiosqlite:///{db_path}"

    @property
    def sync_database_url(self) -> str:
        resolved = self.resolved_database_url
        if resolved.startswith("sqlite+aiosqlite:///"):
            return resolved.replace("sqlite+aiosqlite:///", "sqlite:///")
        return resolved

    @property
    def resolved_prompts_file(self) -> Path:
        path = self.prompts_file or Path("prompts.json")
        return self._resolve_path(path, base=self.resolved_state_dir)

    @property
    def resolved_memory_dir(self) -> Path:
        path = self.memory_dir or Path("memory")
        return self._resolve_path(path, base=self.resolved_state_dir)

    @property
    def resolved_skills_dir(self) -> Path:
        path = self.skills_dir or Path("skills")
        return self._resolve_path(path, base=self.resolved_state_dir)

    @property
    def resolved_workspace_dir(self) -> Path:
        return self._resolve_path(self.workspace_dir, base=Path.cwd())

    @staticmethod
    def _resolve_path(path: Path, base: Path) -> Path:
        expanded = path.expanduser()
        if expanded.is_absolute():
            return expanded.resolve()
        return (base / expanded).resolve()


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()
