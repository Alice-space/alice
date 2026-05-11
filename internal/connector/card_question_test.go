package connector

import (
	"encoding/json"
	"strings"
	"testing"

	llm "github.com/Alice-space/alice/internal/llm"
)

func TestBuildQuestionCard_SingleQuestionSingleSelect(t *testing.T) {
	card := buildQuestionCard("que_001", []llm.QuestionInfo{
		{
			Question: "What is your favorite color?",
			Header:   "Color",
			Options: []llm.QuestionOption{
				{Label: "Red", Description: "Passionate"},
				{Label: "Blue", Description: "Calm"},
			},
			Multiple: false,
		},
	})
	if card == "" {
		t.Fatal("expected non-empty card")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(card), &parsed); err != nil {
		t.Fatalf("card is not valid JSON: %v", err)
	}
	header, _ := parsed["header"].(map[string]any)
	if header == nil {
		t.Fatal("card missing header")
	}
	title, _ := header["title"].(map[string]any)
	if title == nil || title["content"] != "Color" {
		t.Fatalf("unexpected header title: %v", title)
	}

	schema := stringFromMap(parsed, "schema")
	if schema != "2.0" {
		t.Fatalf("unexpected schema: %q", schema)
	}

	t.Logf("card content (valid JSON): %s", card)
}

func TestBuildQuestionCard_MultiSelect(t *testing.T) {
	card := buildQuestionCard("que_002", []llm.QuestionInfo{
		{
			Question: "Select frameworks",
			Header:   "",
			Options: []llm.QuestionOption{
				{Label: "React"},
				{Label: "Vue"},
				{Label: "Svelte"},
			},
			Multiple: true,
		},
	})
	if card == "" {
		t.Fatal("expected non-empty card")
	}
	if !strings.Contains(card, "multi_select_static") {
		t.Fatalf("expected multi_select_static in card, got: %s", card)
	}
	if !strings.Contains(card, "多选") {
		t.Fatalf("expected 多选 label, got: %s", card)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(card), &parsed)
	body, _ := parsed["body"].(map[string]any)
	elements, _ := body["elements"].([]any)
	if len(elements) < 2 {
		t.Fatalf("expected at least 2 body elements, got %d", len(elements))
	}
	t.Logf("multi-select card: %s", card)
}

func TestBuildQuestionCard_MultipleQuestions(t *testing.T) {
	card := buildQuestionCard("que_003", []llm.QuestionInfo{
		{
			Question: "Q1",
			Options:  []llm.QuestionOption{{Label: "A"}, {Label: "B"}},
		},
		{
			Question: "Q2",
			Options:  []llm.QuestionOption{{Label: "X"}, {Label: "Y"}, {Label: "Z"}},
			Multiple: true,
		},
	})
	if card == "" {
		t.Fatal("expected non-empty card")
	}
	if !strings.Contains(card, "\"q0\"") || !strings.Contains(card, "\"q1\"") {
		t.Fatalf("expected q0 and q1 form names: %s", card)
	}
	count := strings.Count(card, "select_static")
	if count != 2 {
		t.Fatalf("expected 2 select elements, got %d: %s", count, card)
	}
}

func TestBuildQuestionCard_NoQuestions(t *testing.T) {
	card := buildQuestionCard("que_empty", nil)
	if card != "" {
		t.Fatalf("expected empty card for nil questions, got: %s", card)
	}
	card = buildQuestionCard("que_empty", []llm.QuestionInfo{})
	if card != "" {
		t.Fatalf("expected empty card for empty questions, got: %s", card)
	}
}

func TestBuildQuestionCard_TitleFallback(t *testing.T) {
	card := buildQuestionCard("q_001", []llm.QuestionInfo{
		{Question: "Hello?", Options: []llm.QuestionOption{{Label: "Yes"}}},
	})
	if !strings.Contains(card, "请回答以下问题") {
		t.Fatalf("expected fallback title, got: %s", card)
	}

	card2 := buildQuestionCard("q_002", []llm.QuestionInfo{
		{Question: "Q1", Options: []llm.QuestionOption{{Label: "A"}}},
		{Question: "Q2", Options: []llm.QuestionOption{{Label: "B"}}},
	})
	if !strings.Contains(card2, "请回答 2 个问题") {
		t.Fatalf("expected multi-question title, got: %s", card2)
	}
}

func TestBuildQuestionCard_EmptyOptionsShowsPlaceholder(t *testing.T) {
	card := buildQuestionCard("q_e", []llm.QuestionInfo{
		{Question: "No options?", Options: nil},
	})
	if !strings.Contains(card, "无可用选项") {
		t.Fatalf("expected placeholder for empty options, got: %s", card)
	}
}

func TestParseQuestionAnswers_SingleSelect(t *testing.T) {
	questions := []llm.QuestionInfo{
		{
			Question: "Q1",
			Options:  []llm.QuestionOption{{Label: "A"}, {Label: "B"}},
			Multiple: false,
		},
	}
	answers, err := parseQuestionAnswers(map[string]any{"q0": "A"}, questions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(answers) != 1 || len(answers[0]) != 1 || answers[0][0] != "A" {
		t.Fatalf("unexpected answers: %v", answers)
	}
}

func TestParseQuestionAnswers_MultiSelect(t *testing.T) {
	questions := []llm.QuestionInfo{
		{
			Question: "Q1",
			Options:  []llm.QuestionOption{{Label: "A"}, {Label: "B"}, {Label: "C"}},
			Multiple: true,
		},
	}
	answers, err := parseQuestionAnswers(map[string]any{"q0": []any{"A", "C"}}, questions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(answers) != 1 || len(answers[0]) != 2 {
		t.Fatalf("unexpected answers: %v", answers)
	}
	if answers[0][0] != "A" || answers[0][1] != "C" {
		t.Fatalf("unexpected answer values: %v", answers[0])
	}
}

func TestParseQuestionAnswers_MissingRequired(t *testing.T) {
	questions := []llm.QuestionInfo{
		{
			Question: "Q1",
			Options:  []llm.QuestionOption{{Label: "A"}},
			Multiple: false,
		},
	}
	_, err := parseQuestionAnswers(map[string]any{}, questions)
	if err == nil {
		t.Fatal("expected error for missing required answer")
	}
}

func TestParseQuestionAnswers_MultiSelectEmptyAllowed(t *testing.T) {
	questions := []llm.QuestionInfo{
		{
			Question: "Q1",
			Options:  []llm.QuestionOption{{Label: "A"}},
			Multiple: true,
		},
	}
	answers, err := parseQuestionAnswers(map[string]any{}, questions)
	if err != nil {
		t.Fatalf("unexpected error for empty multi-select: %v", err)
	}
	if len(answers) != 1 || len(answers[0]) != 0 {
		t.Fatalf("expected empty answer for multi-select, got: %v", answers)
	}
}

func TestParseQuestionAnswers_MixedSingleAndMulti(t *testing.T) {
	questions := []llm.QuestionInfo{
		{
			Question: "single",
			Options:  []llm.QuestionOption{{Label: "S"}},
			Multiple: false,
		},
		{
			Question: "multi",
			Options:  []llm.QuestionOption{{Label: "M1"}, {Label: "M2"}},
			Multiple: true,
		},
	}
	answers, err := parseQuestionAnswers(map[string]any{
		"q0": "S",
		"q1": []any{"M1", "M2"},
	}, questions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("expected 2 answers, got %d", len(answers))
	}
	if answers[0][0] != "S" {
		t.Fatalf("unexpected single answer: %v", answers[0])
	}
	if len(answers[1]) != 2 || answers[1][0] != "M1" || answers[1][1] != "M2" {
		t.Fatalf("unexpected multi answer: %v", answers[1])
	}
}

func TestFormValueToString_String(t *testing.T) {
	if s := formValueToString("hello"); s != "hello" {
		t.Fatalf("expected 'hello', got %q", s)
	}
}

func TestFormValueToString_Float64(t *testing.T) {
	if s := formValueToString(float64(42)); s != "42" {
		t.Fatalf("expected '42', got %q", s)
	}
}

func TestFormValueToString_Null(t *testing.T) {
	if s := formValueToString(nil); s != "" {
		t.Fatalf("expected empty, got %q", s)
	}
}

func TestFormValueToStringSlice_AnySlice(t *testing.T) {
	result := formValueToStringSlice([]any{"a", "b", float64(42)})
	if len(result) != 3 || result[0] != "a" || result[1] != "b" || result[2] != "42" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestFormValueToStringSlice_StringSlice(t *testing.T) {
	result := formValueToStringSlice([]string{"x", "y"})
	if len(result) != 2 || result[0] != "x" || result[1] != "y" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestFormValueToStringSlice_JSONArrayString(t *testing.T) {
	result := formValueToStringSlice(`["a","b","c"]`)
	if len(result) != 3 || result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestFormValueToStringSlice_SingleString(t *testing.T) {
	result := formValueToStringSlice("single")
	if len(result) != 1 || result[0] != "single" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestFormValueToStringSlice_Nil(t *testing.T) {
	result := formValueToStringSlice(nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestQuestionCardTitle(t *testing.T) {
	title := questionCardTitle([]llm.QuestionInfo{
		{Header: "Choose DB", Question: "Which database?"},
	})
	if title != "Choose DB" {
		t.Fatalf("expected 'Choose DB', got %q", title)
	}

	title = questionCardTitle([]llm.QuestionInfo{
		{Header: "", Question: "Q?"},
	})
	if title != "请回答以下问题" {
		t.Fatalf("expected fallback, got %q", title)
	}

	title = questionCardTitle([]llm.QuestionInfo{
		{Header: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	})
	if len(title) > 30 {
		t.Fatalf("header not truncated: %q (%d chars)", title, len(title))
	}
}

func TestQuestionCardStateStoreLoad(t *testing.T) {
	requestID := "test_que_001"
	state := questionCardState{
		RequestID: requestID,
		Questions: []llm.QuestionInfo{
			{Question: "Q?", Options: []llm.QuestionOption{{Label: "A"}}},
		},
	}
	storeQuestionState(requestID, state)
	loaded, ok := loadQuestionState(requestID)
	if !ok {
		t.Fatal("expected to find stored state")
	}
	if loaded.RequestID != requestID || len(loaded.Questions) != 1 {
		t.Fatalf("unexpected loaded state: %+v", loaded)
	}
	pendingQuestionStates.Delete(requestID)
}

func TestQuestionCardStateLoadMissing(t *testing.T) {
	_, ok := loadQuestionState("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func stringFromMap(m map[string]any, key string) string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	s, _ := raw.(string)
	return s
}
