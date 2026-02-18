FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    ca-certificates \
    nodejs \
    npm \
  && rm -rf /var/lib/apt/lists/*

# Codex CLI for ChatGPT Plus bridge mode
RUN npm install -g @openai/codex

WORKDIR /workspace

COPY pyproject.toml README.md ./
COPY app ./app
RUN pip install --no-cache-dir .

COPY . .

EXPOSE 8000
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
