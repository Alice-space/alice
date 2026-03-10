package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ApplyJSONMergePatch(base, patch json.RawMessage) (json.RawMessage, error) {
	baseValue := any(map[string]any{})
	if len(strings.TrimSpace(string(base))) > 0 {
		if err := json.Unmarshal(base, &baseValue); err != nil {
			return nil, fmt.Errorf("invalid base document: %w", err)
		}
	}
	if len(strings.TrimSpace(string(patch))) == 0 {
		out, err := json.Marshal(baseValue)
		if err != nil {
			return nil, err
		}
		return out, nil
	}

	var patchValue any
	if err := json.Unmarshal(patch, &patchValue); err != nil {
		return nil, fmt.Errorf("invalid merge patch: %w", err)
	}
	merged := mergePatchValue(baseValue, patchValue)
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func ValidatePatchedInputSchema(schemaID string, document json.RawMessage) error {
	var parsed any
	if err := json.Unmarshal(document, &parsed); err != nil {
		return fmt.Errorf("invalid patched input document: %w", err)
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return fmt.Errorf("patched input must be a json object")
	}
	if len(obj) == 0 {
		return fmt.Errorf("patched input must not be empty")
	}
	switch strings.TrimSpace(schemaID) {
	case "recovery.schedule_trigger":
		reason, _ := obj["reason"].(string)
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("recovery.schedule_trigger requires non-empty reason")
		}
	}
	return nil
}

func mergePatchValue(target any, patch any) any {
	patchObj, isPatchObj := patch.(map[string]any)
	if !isPatchObj {
		return patch
	}

	targetObj, isTargetObj := target.(map[string]any)
	if !isTargetObj {
		targetObj = map[string]any{}
	}
	for key, value := range patchObj {
		if value == nil {
			delete(targetObj, key)
			continue
		}
		targetObj[key] = mergePatchValue(targetObj[key], value)
	}
	return targetObj
}
