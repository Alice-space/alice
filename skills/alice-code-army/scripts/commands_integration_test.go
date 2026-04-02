package scripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type commandTestCampaign struct {
	Status           string `json:"status"`
	CampaignRepoPath string `json:"campaign_repo_path,omitempty"`
}

type commandTestState struct {
	Status           string                   `json:"status"`
	Campaign         commandTestCampaign      `json:"campaign"`
	LastTaskGuidance *commandTestTaskGuidance `json:"last_task_guidance,omitempty"`
}

type commandTestTaskGuidance struct {
	CampaignID string `json:"campaign_id"`
	TaskID     string `json:"task_id"`
	Action     string `json:"action"`
	Guidance   string `json:"guidance"`
}

func TestAliceCodeArmyApplyCommandResumeInfersPlanReviewPending(t *testing.T) {
	requireShellDeps(t)

	tempDir := t.TempDir()
	repoPath := filepath.Join(tempDir, "campaign-repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo path failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "campaign.md"), []byte("plan_status: plan_review_pending\nplan_round: 2\n"), 0o644); err != nil {
		t.Fatalf("write campaign.md failed: %v", err)
	}

	statePath := filepath.Join(tempDir, "state.json")
	writeCommandTestState(t, statePath, commandTestState{
		Status: "ok",
		Campaign: commandTestCampaign{
			Status:           "hold",
			CampaignRepoPath: repoPath,
		},
	})

	cmd := exec.Command("bash", scriptPath(t), "apply-command", "camp_demo", "/alice resume", "feishu")
	cmd.Env = append(os.Environ(),
		"ALICE_RUNTIME_BIN="+writeFakeAliceBinary(t, tempDir),
		"ALICE_TEST_STATE_FILE="+statePath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("apply-command resume failed: %v\n%s", err, output)
	}

	state := readCommandTestState(t, statePath)
	if state.Campaign.Status != "plan_review_pending" {
		t.Fatalf("expected resume to infer plan_review_pending, got %q", state.Campaign.Status)
	}
	if !strings.Contains(string(output), "plan_review_pending") {
		t.Fatalf("expected command output to include patched status, got %q", output)
	}
}

func TestAliceCodeArmyApplyCommandResumeRejectsTerminalCampaign(t *testing.T) {
	requireShellDeps(t)

	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	writeCommandTestState(t, statePath, commandTestState{
		Status: "ok",
		Campaign: commandTestCampaign{
			Status: "completed",
		},
	})

	cmd := exec.Command("bash", scriptPath(t), "apply-command", "camp_demo", "/alice resume", "feishu")
	cmd.Env = append(os.Environ(),
		"ALICE_RUNTIME_BIN="+writeFakeAliceBinary(t, tempDir),
		"ALICE_TEST_STATE_FILE="+statePath,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected terminal campaign resume to fail, output=%s", output)
	}
	if !strings.Contains(string(output), "cannot resume terminal campaign") {
		t.Fatalf("expected terminal campaign error, got %q", output)
	}
}

func TestAliceCodeArmyApplyCommandGuideTaskRoutesToTaskGuidance(t *testing.T) {
	requireShellDeps(t)

	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	writeCommandTestState(t, statePath, commandTestState{
		Status: "ok",
		Campaign: commandTestCampaign{
			Status: "running",
		},
	})

	cmd := exec.Command("bash", scriptPath(t), "apply-command", "camp_demo", "/alice guide-task T306 accept Accept current Mode B handoff", "feishu")
	cmd.Env = append(os.Environ(),
		"ALICE_RUNTIME_BIN="+writeFakeAliceBinary(t, tempDir),
		"ALICE_TEST_STATE_FILE="+statePath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("apply-command guide-task failed: %v\n%s", err, output)
	}

	state := readCommandTestState(t, statePath)
	if state.LastTaskGuidance == nil {
		t.Fatalf("expected fake runtime to record task guidance, state=%+v", state)
	}
	if state.LastTaskGuidance.CampaignID != "camp_demo" || state.LastTaskGuidance.TaskID != "T306" || state.LastTaskGuidance.Action != "accept" {
		t.Fatalf("unexpected task guidance payload: %+v", state.LastTaskGuidance)
	}
	if state.LastTaskGuidance.Guidance != "Accept current Mode B handoff" {
		t.Fatalf("unexpected guidance text: %+v", state.LastTaskGuidance)
	}
	if !strings.Contains(string(output), "\"task_id\": \"T306\"") {
		t.Fatalf("expected task-guidance output to include task id, got %q", output)
	}
}

func scriptPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	return filepath.Join(wd, "alice-code-army.sh")
}

func writeFakeAliceBinary(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "alice")
	content := `#!/usr/bin/env bash
set -euo pipefail
state_file="${ALICE_TEST_STATE_FILE:?}"
if [[ "$#" -lt 3 || "$1" != "runtime" || "$2" != "campaigns" ]]; then
  echo "unexpected args: $*" >&2
  exit 1
fi
cmd="$3"
shift 3
case "$cmd" in
  get)
    cat "$state_file"
    ;;
  patch)
    if [[ "$#" -ne 2 ]]; then
      echo "unexpected patch args: $*" >&2
      exit 1
    fi
    patch_json="$2"
    tmp="${state_file}.tmp"
    jq --argjson patch "$patch_json" '.campaign |= (. + $patch)' "$state_file" > "$tmp"
    mv "$tmp" "$state_file"
    cat "$state_file"
    ;;
  task-guidance)
    if [[ "$#" -ne 4 ]]; then
      echo "unexpected task-guidance args: $*" >&2
      exit 1
    fi
    tmp="${state_file}.tmp"
    jq \
      --arg campaign_id "$1" \
      --arg task_id "$2" \
      --arg action "$3" \
      --arg guidance "$4" \
      '.last_task_guidance = {campaign_id:$campaign_id, task_id:$task_id, action:$action, guidance:$guidance}' \
      "$state_file" > "$tmp"
    mv "$tmp" "$state_file"
    jq -n --arg status "ok" --arg task_id "$2" --arg action "$3" '{"status":$status,"task_id":$task_id,"action":$action}'
    ;;
  *)
    echo "unexpected subcommand: $cmd" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake alice binary failed: %v", err)
	}
	return path
}

func writeCommandTestState(t *testing.T, path string, state commandTestState) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state failed: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}
}

func readCommandTestState(t *testing.T, path string) commandTestState {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state failed: %v", err)
	}
	var state commandTestState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal state failed: %v", err)
	}
	return state
}

func requireShellDeps(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell integration test is not supported on windows")
	}
	for _, dep := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(dep); err != nil {
			t.Skipf("missing dependency %s: %v", dep, err)
		}
	}
}
