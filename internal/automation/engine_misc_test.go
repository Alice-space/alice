package automation

import (
	"strings"
	"testing"
	"time"
)

func TestEngine_SetUserTaskTimeout_NonPositiveFallsBackToDefault(t *testing.T) {
	engine := NewEngine(nil, nil)
	engine.SetUserTaskTimeout(0)
	if got := engine.userTaskTimeoutDuration(); got != defaultUserTaskTimeout {
		t.Fatalf("unexpected default timeout: %s", got)
	}
}

func TestRenderActionTemplate_InvalidTemplateReturnsError(t *testing.T) {
	_, err := renderActionTemplate("{{ if }}", time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected renderActionTemplate to return error for invalid template")
	}
	if !strings.Contains(err.Error(), "render action template failed") {
		t.Fatalf("unexpected template error: %v", err)
	}
}

func TestRenderActionTemplate_EmptyInputReturnsEmpty(t *testing.T) {
	got, err := renderActionTemplate("   ", time.Time{})
	if err != nil {
		t.Fatalf("expected nil error for empty template, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty rendered text, got %q", got)
	}
}
