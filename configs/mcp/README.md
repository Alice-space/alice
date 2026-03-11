# MCP Configuration

This directory contains MCP (Model Context Protocol) configuration documentation for Alice.

## Overview

Alice uses MCP via **HTTP transport** to enable structured tool calling for LLM agents. The MCP server is embedded in the Alice binary (`internal/mcp`), eliminating the need for a separate process.

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Alice Agent   │────▶│   kimi CLI       │────▶│  MCP HTTP Server│
│  (internal/agent)│     │  (--mcp-config)  │     │(internal/mcp)   │
└─────────────────┘     └──────────────────┘     └─────────────────┘
```

## Embedded MCP Server

The `internal/mcp` package provides an HTTP-based MCP server using the official Go SDK:

```go
import "alice/internal/mcp"

// Create and start MCP server
server, err := mcp.NewServer(mcp.Config{
    Host: "127.0.0.1",
    Port: 0, // 0 = random available port
})
if err != nil {
    log.Fatal(err)
}

if err := server.Start(); err != nil {
    log.Fatal(err)
}

// Use with agent
agent := agent.NewLocalAgent(agent.Config{
    MCPServer: server,
})
```

## Available Tools

The MCP server provides these tools:

### 1. `submit_promotion_decision`

Used by Reception to classify and route requests.

**Parameters:**
- `intent_kind`: Enum of intent types
- `risk_level`: "low", "medium", or "high"
- `external_write`, `create_persistent_object`, `async`, `multi_step`, `multi_agent`: Boolean flags
- `approval_required`, `budget_required`, `recovery_required`: Boolean flags
- `proposed_workflow_ids`: Array of workflow IDs
- `reason_codes`: Array of reason codes
- `confidence`: Float between 0 and 1

### 2. `submit_direct_answer`

Used for direct query responses.

**Parameters:**
- `answer`: The response text
- `citations`: Optional array of citation sources

### 3. `submit_tool_call`

Used to request tool/MCP invocations.

**Parameters:**
- `tool_name`: Name of the tool to call
- `parameters`: Tool parameters as JSON object

## MCP Configuration Format

Alice generates MCP configuration dynamically for kimi CLI:

```json
{
  "mcpServers": {
    "alice-tools": {
      "transport": "http",
      "url": "http://127.0.0.1:54321"
    }
  }
}
```

This configuration is passed to kimi via the `--mcp-config` flag.

## Tool Output Format

Tools return wrapped JSON:

```json
{
  "type": "promotion_decision",
  "payload": {
    "intent_kind": "direct_query",
    "risk_level": "low",
    ...
  }
}
```

The agent parses this format to extract structured data.

## References

- [MCP Specification](https://modelcontextprotocol.io/)
- [Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [ADR 0007: MCP Tool Calling](../adr/0007-mcp-tool-calling-for-structured-output.md)
