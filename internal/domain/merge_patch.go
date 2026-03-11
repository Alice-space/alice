package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// ApplyJSONMergePatch applies a JSON Merge Patch (RFC 7396) to a base document.
// Uses evanphx/json-patch for proper RFC 6902/7396 compliance.
func ApplyJSONMergePatch(base, patch json.RawMessage) (json.RawMessage, error) {
	// Handle empty base
	if len(strings.TrimSpace(string(base))) == 0 {
		base = []byte("{}")
	}

	// Handle empty patch
	if len(strings.TrimSpace(string(patch))) == 0 {
		return base, nil
	}

	// Use json-patch's MergePatch implementation
	merged, err := jsonpatch.MergePatch(base, patch)
	if err != nil {
		return nil, fmt.Errorf("apply merge patch: %w", err)
	}

	return merged, nil
}

// ValidatePatchedInputSchema validates a patched input document against a schema.
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
