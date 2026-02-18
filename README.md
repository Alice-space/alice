# Alice Portable Private Assistant

Alice is a FastAPI-based private assistant with three trigger sources:

- Feishu webhook (`feishu_message`)
- Scheduled automations (`schedule_fire`)
- Todo queue non-empty worker (`todo_non_empty`)

Core runtime flow:

1. Assemble context (prompt, skills, memory, tools, session window)
2. Ask model provider for one next action (`final_message` or `tool_call`)
3. Execute tool call if needed
4. Stream runtime events over SSE
5. Persist session artifacts in SQLite
6. Append memory to markdown files (`memory/MEMORY.md`, `memory/YYYY-MM-DD.md`)

## Quickstart

```bash
brew install uv
uv python install 3.12
uv venv --python 3.12 .venv
uv pip install --python .venv/bin/python -e '.[dev]'
cp .env.example .env
mkdir -p ~/.alice
# edit .env and set ALICE_STATE_DIR=~/.alice (or any external path)
.venv/bin/uvicorn app.main:app --reload
```

Open [http://localhost:8000](http://localhost:8000) for the ops console.

## Auth

Management APIs require `Authorization: Bearer <ALICE_API_TOKEN>`.

## Approval policy

Configurable via env:

- `ALICE_APPROVAL_MODE=auto_all` (default, standalone-machine friendly)
- `ALICE_APPROVAL_MODE=trusted_only`
- `ALICE_APPROVAL_MODE=explicit_only`

Additional controls:

- `ALICE_APPROVAL_REQUIRED_TOOLS=["http.request"]`
- `ALICE_HTTP_WRITE_REQUIRES_APPROVAL=true`

## Providers

- Primary: `CodexExecProvider` (`codex exec --json --output-schema`)
- Fallback: `OpenAIAPIProvider` (chat completions API key mode)

Health endpoints:

- `GET /api/v1/providers/health`
- `GET /api/v1/providers/codex/login-status`

## Memory

- Long-term: `${ALICE_STATE_DIR}/memory/MEMORY.md`
- Daily journal: `${ALICE_STATE_DIR}/memory/YYYY-MM-DD.md`

Memory writes are protected by file lock and atomic file replacement.

## Decoupled storage

Runtime state is decoupled from the project directory by default:

- DB: `${ALICE_STATE_DIR}/db/alice.db`
- Memory: `${ALICE_STATE_DIR}/memory/`
- Skills: `${ALICE_STATE_DIR}/skills/`

You can override each path independently with `ALICE_DATABASE_URL`, `ALICE_MEMORY_DIR`, and
`ALICE_SKILLS_DIR`.

## Migrations

```bash
alembic upgrade head
```

## Docker

```bash
docker compose up --build
```

In Docker, persistent runtime state is mounted to `/data/alice` via the `alice-state` volume.

## Tests

```bash
pytest
```

## Pre-commit checks

Before each commit, run:

```bash
make check
```

This includes:

- `isort --check-only`
- `black --check`
- `mypy`
- `pytest -q`

Install git pre-commit hooks once:

```bash
make precommit-install
```
