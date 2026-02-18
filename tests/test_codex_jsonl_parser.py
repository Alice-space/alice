from app.providers.codex_exec import parse_codex_jsonl_output


def test_parse_codex_jsonl_output_ignores_non_json_lines() -> None:
    raw = """
warn line
{"type":"thread.started","thread_id":"x"}
{"type":"item.completed","item":{"type":"agent_message","text":"{\\"action_type\\":\\"final_message\\",\\"final_message\\":\\"OK\\",\\"tool_name\\":null,\\"tool_arguments\\":{},\\"reasoning\\":\\"done\\"}"}}
""".strip()

    events, last_text = parse_codex_jsonl_output(raw)
    assert len(events) == 2
    assert last_text is not None
    assert '"action_type"' in last_text
