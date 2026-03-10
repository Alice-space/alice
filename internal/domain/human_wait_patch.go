package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ResolveHumanWaitInputDraft(wait HumanWaitRecordedPayload) (json.RawMessage, error) {
	if len(strings.TrimSpace(string(wait.InputDraft))) > 0 {
		var v any
		if err := json.Unmarshal(wait.InputDraft, &v); err != nil {
			return nil, fmt.Errorf("invalid wait input_draft: %w", err)
		}
		if _, ok := v.(map[string]any); !ok {
			return nil, fmt.Errorf("wait input_draft must be a json object")
		}
		return wait.InputDraft, nil
	}

	if prompt := strings.TrimSpace(wait.PromptRef); prompt != "" {
		key := "note"
		if strings.TrimSpace(wait.InputSchemaID) == "recovery.schedule_trigger" {
			key = "reason"
		}
		doc, err := json.Marshal(map[string]any{key: prompt})
		if err != nil {
			return nil, err
		}
		return doc, nil
	}

	return nil, fmt.Errorf("input draft is unavailable for wait=%s", strings.TrimSpace(wait.HumanWaitID))
}

func ApplyHumanWaitInputPatch(wait HumanWaitRecordedPayload, patch json.RawMessage) (json.RawMessage, error) {
	base, err := ResolveHumanWaitInputDraft(wait)
	if err != nil {
		return nil, err
	}
	merged, err := ApplyJSONMergePatch(base, patch)
	if err != nil {
		return nil, err
	}
	if err := ValidatePatchedInputSchema(wait.InputSchemaID, merged); err != nil {
		return nil, err
	}
	return merged, nil
}
