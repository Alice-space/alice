# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Alice, please do **not** open a public issue.

Send details to the maintainers via a private channel. We will respond within 48 hours with an acknowledgment and a timeline for a fix.

We appreciate responsible disclosure and will credit reporters in the release notes (unless you prefer to remain anonymous).

## Scope

Security concerns include but are not limited to:

- Unauthorized access to bot credentials or runtime tokens
- Exposure of Feishu App Secrets in logs or debug output
- Runtime API authentication bypass
- Prompt injection leading to unintended tool execution
- Denial of service via message flooding

## Best Practices for Operators

- Never commit `config.yaml` containing real credentials
- Use `log_level: debug` only temporarily for troubleshooting — debug logs may contain rendered prompts
- The runtime API listens on a Unix domain socket by default — no network exposure
- Rotate your `runtime_http_token` periodically if exposed
