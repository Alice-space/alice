package runtimeapi

import (
	"testing"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/mcpbridge"
)

func TestBuildTaskFromRequest_UsesWorkSceneLLMProfile(t *testing.T) {
	srv := NewServer(
		"",
		"",
		nil,
		nil,
		nil,
		config.Config{
			LLMProfiles: map[string]config.LLMProfileConfig{
				"work": {
					Provider:        "codex",
					Model:           "gpt-5.4",
					ReasoningEffort: "xhigh",
					Personality:     "pragmatic",
				},
			},
			GroupScenes: config.GroupScenesConfig{
				Work: config.GroupSceneConfig{
					Enabled:    true,
					LLMProfile: "work",
				},
			},
		},
	)
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Title: "heartbeat",
			Schedule: automation.Schedule{
				Type:         automation.ScheduleTypeInterval,
				EverySeconds: 3600,
			},
			Action: automation.Action{
				Prompt: "总结当前状态",
			},
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
			route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: mcpbridge.SessionContext{SessionKey: "chat_id:oc_chat|scene:work|seed:om_1"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.Action.Type != automation.ActionTypeRunLLM {
		t.Fatalf("unexpected action type: %q", task.Action.Type)
	}
	if task.Action.Model != "gpt-5.4" {
		t.Fatalf("unexpected model: %q", task.Action.Model)
	}
	if task.Action.ReasoningEffort != "xhigh" {
		t.Fatalf("unexpected reasoning effort: %q", task.Action.ReasoningEffort)
	}
	if task.Action.Personality != "pragmatic" {
		t.Fatalf("unexpected personality: %q", task.Action.Personality)
	}
}

func TestBuildTaskFromRequest_PreservesExplicitRunLLMSelectors(t *testing.T) {
	srv := NewServer(
		"",
		"",
		nil,
		nil,
		nil,
		config.Config{
			LLMProfiles: map[string]config.LLMProfileConfig{
				"work": {
					Provider:        "codex",
					Model:           "gpt-5.4",
					ReasoningEffort: "xhigh",
					Personality:     "pragmatic",
				},
			},
			GroupScenes: config.GroupScenesConfig{
				Work: config.GroupSceneConfig{
					Enabled:    true,
					LLMProfile: "work",
				},
			},
		},
	)
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Schedule: automation.Schedule{
				Type:         automation.ScheduleTypeInterval,
				EverySeconds: 3600,
			},
			Action: automation.Action{
				Prompt:          "总结当前状态",
				Model:           "gpt-5.4-mini",
				ReasoningEffort: "low",
				Personality:     "friendly",
			},
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
			route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: mcpbridge.SessionContext{SessionKey: "chat_id:oc_chat|scene:work|seed:om_1"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.Action.Model != "gpt-5.4-mini" {
		t.Fatalf("unexpected model override: %q", task.Action.Model)
	}
	if task.Action.ReasoningEffort != "low" {
		t.Fatalf("unexpected reasoning effort override: %q", task.Action.ReasoningEffort)
	}
	if task.Action.Personality != "friendly" {
		t.Fatalf("unexpected personality override: %q", task.Action.Personality)
	}
}

func TestBuildTaskFromRequest_InferRunWorkflowAndSetSessionKey(t *testing.T) {
	srv := NewServer(
		"",
		"",
		nil,
		nil,
		nil,
		config.Config{
			LLMProfiles: map[string]config.LLMProfileConfig{
				"work": {
					Provider:        "codex",
					Model:           "gpt-5.4",
					Profile:         "work-profile",
					ReasoningEffort: "xhigh",
					Personality:     "pragmatic",
				},
			},
			GroupScenes: config.GroupScenesConfig{
				Work: config.GroupSceneConfig{
					Enabled:    true,
					LLMProfile: "work",
				},
			},
		},
	)
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Schedule: automation.Schedule{
				Type:         automation.ScheduleTypeInterval,
				EverySeconds: 900,
			},
			Action: automation.Action{
				Workflow: "code_army",
				Prompt:   "/alice reconcile campaign camp_x",
			},
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
			route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: mcpbridge.SessionContext{SessionKey: "chat_id:oc_chat|scene:work|thread:omt_1|message:om_2"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.Action.Type != automation.ActionTypeRunWorkflow {
		t.Fatalf("unexpected workflow action type: %q", task.Action.Type)
	}
	if task.Action.Model != "gpt-5.4" {
		t.Fatalf("unexpected workflow model: %q", task.Action.Model)
	}
	if task.Action.Profile != "work-profile" {
		t.Fatalf("unexpected workflow profile: %q", task.Action.Profile)
	}
	if task.Action.ReasoningEffort != "xhigh" {
		t.Fatalf("unexpected workflow reasoning effort: %q", task.Action.ReasoningEffort)
	}
	if task.Action.Personality != "pragmatic" {
		t.Fatalf("unexpected workflow personality: %q", task.Action.Personality)
	}
	if task.Action.SessionKey != "chat_id:oc_chat|scene:work|thread:omt_1" {
		t.Fatalf("unexpected workflow session key: %q", task.Action.SessionKey)
	}
}
