# syntax=docker/dockerfile:1

ARG GO_VERSION=1.25
ARG NODE_VERSION=22

FROM golang:${GO_VERSION}-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/alice ./cmd/connector

FROM node:${NODE_VERSION}-bookworm-slim AS runtime
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    ripgrep \
    tini \
    && rm -rf /var/lib/apt/lists/*

# Install both supported LLM CLIs for runtime switching.
RUN npm install -g @openai/codex @anthropic-ai/claude-code && \
    npm cache clean --force

RUN useradd --create-home --shell /bin/bash alice

ENV HOME=/home/alice \
    ALICE_HOME=/home/alice/.alice \
    CODEX_HOME=/home/alice/.codex \
    CLAUDE_HOME=/home/alice/.claude

WORKDIR /app

COPY --from=builder /out/alice /usr/local/bin/alice
COPY config.example.yaml /app/config.yaml
COPY skills /app/skills

RUN chown -R alice:alice /app /home/alice
USER alice

# Pre-mark Claude Code onboarding so first run in container is non-interactive.
RUN echo '{' > /home/alice/.claude.json && \
    echo '  "hasCompletedOnboarding": true' >> /home/alice/.claude.json && \
    echo '}' >> /home/alice/.claude.json

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["alice", "-c", "/app/config.yaml"]
