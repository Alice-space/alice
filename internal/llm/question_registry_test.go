package llm

import (
	"encoding/json"
	"testing"
)

func TestRegisterPendingQuestion(t *testing.T) {
	RegisterPendingQuestion("que_test_001", "http://127.0.0.1:1234/")
	baseURL, ok := LookupPendingQuestion("que_test_001")
	if !ok {
		t.Fatal("expected to find registered question")
	}
	if baseURL != "http://127.0.0.1:1234" {
		t.Fatalf("unexpected baseURL: %q", baseURL)
	}
	RemovePendingQuestion("que_test_001")

	_, ok = LookupPendingQuestion("que_test_001")
	if ok {
		t.Fatal("expected question to be removed")
	}
}

func TestRegisterPendingQuestion_EmptyInput(t *testing.T) {
	RegisterPendingQuestion("", "http://example.com")
	_, ok := LookupPendingQuestion("")
	if ok {
		t.Fatal("expected empty requestID to be ignored")
	}

	RegisterPendingQuestion("que_empty_url", "")
	_, ok = LookupPendingQuestion("que_empty_url")
	if ok {
		t.Fatal("expected empty baseURL to be ignored")
	}
}

func TestLookupPendingQuestion_Missing(t *testing.T) {
	_, ok := LookupPendingQuestion("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestParseOpenCodeQuestions(t *testing.T) {
	properties := map[string]any{
		"requestID": "que_parsed",
		"questions": []any{
			map[string]any{
				"question": "What database?",
				"header":   "DB Choice",
				"options": []any{
					map[string]any{"label": "PostgreSQL", "description": "ACID"},
					map[string]any{"label": "SQLite", "description": "Lightweight"},
				},
				"multiple": false,
				"custom":   true,
			},
		},
	}
	questions := parseOpenCodeQuestions(properties)
	if len(questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(questions))
	}
	q := questions[0]
	if q.Question != "What database?" {
		t.Fatalf("expected question text, got %q", q.Question)
	}
	if q.Header != "DB Choice" {
		t.Fatalf("expected header, got %q", q.Header)
	}
	if len(q.Options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(q.Options))
	}
	if q.Options[0].Label != "PostgreSQL" {
		t.Fatalf("expected PostgreSQL, got %q", q.Options[0].Label)
	}
	if q.Multiple {
		t.Fatal("expected single-select")
	}
	if !q.Custom {
		t.Fatal("expected custom=true")
	}
}

func TestParseOpenCodeQuestions_MultipleQuestions(t *testing.T) {
	properties := map[string]any{
		"questions": []any{
			map[string]any{
				"question": "Q1",
				"options": []any{
					map[string]any{"label": "A"},
				},
			},
			map[string]any{
				"question": "Q2",
				"multiple": true,
				"options": []any{
					map[string]any{"label": "X"},
					map[string]any{"label": "Y"},
				},
			},
		},
	}
	questions := parseOpenCodeQuestions(properties)
	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}
	if questions[0].Question != "Q1" {
		t.Fatalf("unexpected Q1: %q", questions[0].Question)
	}
	if !questions[1].Multiple {
		t.Fatal("expected Q2 to be multiple")
	}
}

func TestParseOpenCodeQuestions_Empty(t *testing.T) {
	questions := parseOpenCodeQuestions(map[string]any{})
	if questions != nil {
		t.Fatalf("expected nil, got %v", questions)
	}
}

func TestParseOpenCodeQuestions_InvalidJSON(t *testing.T) {
	questions := parseOpenCodeQuestions(map[string]any{
		"questions": "not an array",
	})
	if questions != nil {
		t.Fatalf("expected nil for invalid, got %v", questions)
	}
}

func TestQuestionInfoJSONRoundtrip(t *testing.T) {
	original := TurnQuestion{
		RequestID: "que_roundtrip",
		Questions: []QuestionInfo{
			{
				Question: "What?",
				Header:   "Q",
				Options:  []QuestionOption{{Label: "A", Description: "desc"}},
				Multiple: false,
				Custom:   true,
			},
		},
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var restored TurnQuestion
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if restored.RequestID != original.RequestID || len(restored.Questions) != 1 {
		t.Fatalf("roundtrip mismatch: %+v", restored)
	}
	q := restored.Questions[0]
	if q.Question != "What?" || !q.Custom {
		t.Fatalf("question field mismatch: %+v", q)
	}
}
