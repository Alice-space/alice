# ADR 0007: MCP Tool Calling for Structured Output

## Status

Accepted

## Context

The Alice system requires LLM agents to produce structured outputs (e.g., `PromotionDecision`, `DirectAnswer`) for routing and decision-making. Previously, we used JSON code blocks in free-text responses, which required fragile text parsing and had issues with:

1. LLMs outputting explanatory text before/after the JSON
2. Inconsistent formatting (markdown code blocks vs raw JSON)
3. Difficulty validating output structure before parsing
4. No clear separation between reasoning and structured output

## Decision

We will use the **Model Context Protocol (MCP)** with forced tool calling for all structured LLM outputs.

### Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Alice Agent   │────▶│   kimi CLI       │────▶│  MCP HTTP Server│
│  (internal/agent)│     │  (--mcp-config)  │     │(internal/mcp)   │
└─────────────────┘     └──────────────────┘     └─────────────────┘
        │                                               ▲
        │                                               │ HTTP
        ▼                                               │
┌─────────────────┐                          ┌─────────────────┐
│ StructuredOutput│◀─────────────────────────│ Tool Call Result│
│   (map[string]  │                          │  (submit_* tools)│
│   interface{})  │                          │                 │
└─────────────────┘                          └─────────────────┘
```

### Components

1. **MCP HTTP Server** (`internal/mcp`)
   - Embedded HTTP server using official MCP Go SDK
   - Provides three tools:
     - `submit_promotion_decision`: For Reception to submit routing decisions
     - `submit_direct_answer`: For direct query responses
     - `submit_tool_call`: For requesting tool/MCP invocations
   - Uses `StreamableHTTPHandler` for HTTP-based MCP transport
   - Outputs wrapped JSON with `type` and `payload` fields

2. **Agent Integration** (`internal/agent/local.go`)
   - Connects to embedded MCP server via HTTP URL
   - Passes MCP config to kimi via `--mcp-config` flag
   - Parses tool call outputs from MCP server
   - Returns structured data in `ExecuteResult.StructuredOutput`

3. **Prompt Templates**
   - Updated to instruct LLM to use tools instead of outputting JSON
   - Emphasizes "MUST use tools" constraint

### Tool Schemas

#### submit_promotion_decision

```json
{
  "name": "submit_promotion_decision",
  "parameters": {
    "type": "object",
    "properties": {
      "intent_kind": { "enum": ["direct_query", "issue_delivery", ...] },
      "risk_level": { "enum": ["low", "medium", "high"] },
      "external_write": { "type": "boolean" },
      "create_persistent_object": { "type": "boolean" },
      "async": { "type": "boolean" },
      "multi_step": { "type": "boolean" },
      "multi_agent": { "type": "boolean" },
      "approval_required": { "type": "boolean" },
      "budget_required": { "type": "boolean" },
      "recovery_required": { "type": "boolean" },
      "proposed_workflow_ids": { "type": "array", "items": { "type": "string" } },
      "reason_codes": { "type": "array", "items": { "type": "string" } },
      "confidence": { "type": "number", "minimum": 0, "maximum": 1 }
    },
    "required": ["intent_kind", "risk_level", ...]
  }
}
```

## Consequences

### Positive

1. **Structured by Design**: LLM must use defined tools, cannot output free text
2. **Schema Validation**: MCP SDK validates parameters against JSON Schema
3. **Clear Separation**: Reasoning happens in thinking, output happens via tools
4. **Extensible**: Adding new output types means adding new tools
5. **Single Binary**: MCP server is embedded in Alice, no separate binary needed
6. **Network-based**: HTTP transport allows flexible deployment

### Negative

1. **Dependency on kimi MCP support**: Requires kimi CLI with `--mcp-config` flag
2. **Network overhead**: HTTP transport has slight overhead vs stdio
3. **Port management**: Need to manage dynamic port allocation

### Migration

- Old: Parse `extractJSON(output)` from markdown code blocks
- New: Use `result.StructuredOutput` directly from MCP tool calls
- Fallback: If MCP fails, fall back to simple decision heuristics

## References

- [MCP Specification](https://modelcontextprotocol.io/)
- [Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- `internal/mcp/server.go` (Embedded MCP HTTP server)
- `internal/agent/local.go` (MCP client integration)
- `internal/prompts/templates/local_agent_output_format.tmpl`
