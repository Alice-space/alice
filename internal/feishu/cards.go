package feishu

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"alice/internal/domain"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type OutboundMessage struct {
	MsgType string
	Content string
}

func buildApprovalCardMessage(secret []byte, approval domain.ApprovalRequestOpenedPayload, producedAt time.Time) (OutboundMessage, error) {
	actions := approvalButtons(secret, approval, producedAt)
	if len(actions) == 0 {
		return OutboundMessage{}, fmt.Errorf("approval %s has no supported feishu actions", strings.TrimSpace(approval.ApprovalRequestID))
	}

	card := map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"template": approvalHeaderTemplate(approval.GateType),
			"title": map[string]any{
				"tag":     "plain_text",
				"content": approvalHeaderTitle(approval.GateType),
			},
		},
		"elements": []any{
			map[string]any{
				"tag":     "markdown",
				"content": approvalMarkdown(approval),
			},
			map[string]any{
				"tag":     "action",
				"actions": actions,
			},
		},
	}
	content, err := json.Marshal(card)
	if err != nil {
		return OutboundMessage{}, err
	}
	return OutboundMessage{MsgType: larkim.MsgTypeInteractive, Content: string(content)}, nil
}

func buildHumanWaitMessage(secret []byte, wait domain.HumanWaitRecordedPayload, producedAt time.Time) (OutboundMessage, error) {
	elements := []any{
		map[string]any{
			"tag":     "markdown",
			"content": humanWaitMarkdown(wait),
		},
	}

	inputs, supportsForm := humanWaitInputElements(wait)
	actions, requiresForm, err := humanWaitButtons(secret, wait, producedAt)
	if err != nil {
		return OutboundMessage{}, err
	}
	if requiresForm && !supportsForm {
		text := fmt.Sprintf("Human wait %s requires structured input; complete it via the human-action API.", strings.TrimSpace(wait.HumanWaitID))
		return OutboundMessage{MsgType: larkim.MsgTypeText, Content: larkim.NewTextMsgBuilder().Text(text).Build()}, nil
	}
	if len(inputs) > 0 {
		for _, input := range inputs {
			elements = append(elements, input)
		}
	}
	if len(actions) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "action",
			"actions": actions,
		})
	}

	card := map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"template": waitHeaderTemplate(wait.WaitingReason),
			"title": map[string]any{
				"tag":     "plain_text",
				"content": waitHeaderTitle(wait.WaitingReason),
			},
		},
		"elements": elements,
	}
	content, err := json.Marshal(card)
	if err != nil {
		return OutboundMessage{}, err
	}
	return OutboundMessage{MsgType: larkim.MsgTypeInteractive, Content: string(content)}, nil
}

func approvalButtons(secret []byte, approval domain.ApprovalRequestOpenedPayload, producedAt time.Time) []map[string]any {
	actions := make([]map[string]any, 0, 2)
	for _, spec := range approvalActionSpecs(approval.GateType) {
		token, decisionHash, err := buildHumanActionToken(secret, domain.HumanActionClaims{
			ActionKind:        spec.Kind,
			TaskID:            strings.TrimSpace(approval.TaskID),
			ApprovalRequestID: strings.TrimSpace(approval.ApprovalRequestID),
			StepExecutionID:   strings.TrimSpace(approval.StepExecutionID),
			WaitingReason:     spec.WaitingReason,
			DecisionHash:      stableDecisionHash("approval", approval.ApprovalRequestID, approval.TaskID, approval.StepExecutionID, spec.Kind),
			Nonce:             stableNonce("approval", approval.ApprovalRequestID, spec.Kind),
			ExpiresAt:         tokenExpiry(approval.DeadlineAt, producedAt),
		})
		if err != nil {
			continue
		}
		actions = append(actions, actionButton(spec.Label, spec.Style, map[string]any{
			ActionTokenValueKey:     token,
			"action_kind":           spec.Kind,
			"decision_hash":         decisionHash,
			"approval_request_id":   strings.TrimSpace(approval.ApprovalRequestID),
			"task_id":               strings.TrimSpace(approval.TaskID),
			"step_execution_id":     strings.TrimSpace(approval.StepExecutionID),
			"source_ref":            "approval:" + strings.TrimSpace(approval.ApprovalRequestID),
			"human_action_scope":    "approval",
			"human_action_deadline": formatDeadline(approval.DeadlineAt),
		}))
	}
	return actions
}

func humanWaitButtons(secret []byte, wait domain.HumanWaitRecordedPayload, producedAt time.Time) ([]map[string]any, bool, error) {
	actions := make([]map[string]any, 0, len(wait.ResumeOptions))
	requiresForm := false
	for _, option := range wait.ResumeOptions {
		kind := string(domain.NormalizeHumanActionKind(option))
		claims := domain.HumanActionClaims{
			ActionKind:      kind,
			TaskID:          strings.TrimSpace(wait.TaskID),
			HumanWaitID:     strings.TrimSpace(wait.HumanWaitID),
			StepExecutionID: strings.TrimSpace(wait.StepExecutionID),
			WaitingReason:   strings.TrimSpace(wait.WaitingReason),
			DecisionHash:    stableDecisionHash("wait", wait.HumanWaitID, wait.TaskID, wait.StepExecutionID, kind),
			Nonce:           stableNonce("wait", wait.HumanWaitID, kind),
			ExpiresAt:       tokenExpiry(wait.DeadlineAt, producedAt),
		}
		value := map[string]any{
			"action_kind":           kind,
			"human_wait_id":         strings.TrimSpace(wait.HumanWaitID),
			"task_id":               strings.TrimSpace(wait.TaskID),
			"step_execution_id":     strings.TrimSpace(wait.StepExecutionID),
			"waiting_reason":        strings.TrimSpace(wait.WaitingReason),
			"input_schema_id":       strings.TrimSpace(wait.InputSchemaID),
			"source_ref":            "wait:" + strings.TrimSpace(wait.HumanWaitID),
			"human_action_scope":    "human_wait",
			"human_action_deadline": formatDeadline(wait.DeadlineAt),
		}

		switch kind {
		case string(domain.HumanActionProvideInput):
			requiresForm = true
			token, decisionHash, err := buildHumanActionToken(secret, claims)
			if err != nil {
				return nil, false, err
			}
			value[ActionTokenValueKey] = token
			value["decision_hash"] = decisionHash
			actions = append(actions, actionButton("Submit", "primary", value))
		case string(domain.HumanActionResumeRecovery):
			token, decisionHash, err := buildHumanActionToken(secret, claims)
			if err != nil {
				return nil, false, err
			}
			value[ActionTokenValueKey] = token
			value["decision_hash"] = decisionHash
			actions = append(actions, actionButton("Resume", "primary", value))
		case string(domain.HumanActionCancel):
			claims.WaitingReason = ""
			token, decisionHash, err := buildHumanActionToken(secret, claims)
			if err != nil {
				return nil, false, err
			}
			value[ActionTokenValueKey] = token
			value["decision_hash"] = decisionHash
			actions = append(actions, actionButton("Cancel", "danger", value))
		}
	}
	return actions, requiresForm, nil
}

func humanWaitInputElements(wait domain.HumanWaitRecordedPayload) ([]map[string]any, bool) {
	draft, err := domain.ResolveHumanWaitInputDraft(wait)
	if err != nil {
		return nil, false
	}
	var fields map[string]any
	if err := json.Unmarshal(draft, &fields); err != nil {
		return nil, false
	}
	keys := make([]string, 0, len(fields))
	for key, raw := range fields {
		if _, ok := raw.(string); ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return nil, false
	}
	elements := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		text := strings.TrimSpace(fields[key].(string))
		elements = append(elements, map[string]any{
			"tag":  "textarea",
			"name": key,
			"label": map[string]any{
				"tag":     "plain_text",
				"content": humanizeFieldKey(key),
			},
			"placeholder": map[string]any{
				"tag":     "plain_text",
				"content": humanizeFieldKey(key),
			},
			"value": text,
		})
	}
	return elements, true
}

func approvalActionSpecs(gateType string) []cardActionSpec {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case string(domain.GateTypeApproval):
		return []cardActionSpec{{Kind: string(domain.HumanActionApprove), Label: "Approve", Style: "primary"}, {Kind: string(domain.HumanActionReject), Label: "Reject", Style: "danger"}}
	case string(domain.GateTypeConfirmation), string(domain.GateTypeEvaluation):
		return []cardActionSpec{{Kind: string(domain.HumanActionConfirm), Label: "Confirm", Style: "primary"}, {Kind: string(domain.HumanActionReject), Label: "Reject", Style: "danger"}}
	case string(domain.GateTypeBudget):
		return []cardActionSpec{{Kind: string(domain.HumanActionResumeBudget), Label: "Resume Budget", Style: "primary", WaitingReason: string(domain.WaitingReasonBudget)}, {Kind: string(domain.HumanActionReject), Label: "Reject", Style: "danger"}}
	default:
		return nil
	}
}

type cardActionSpec struct {
	Kind          string
	Label         string
	Style         string
	WaitingReason string
}

func buildHumanActionToken(secret []byte, claims domain.HumanActionClaims) (string, string, error) {
	token, err := domain.SignHumanActionTokenV1(secret, claims)
	if err != nil {
		return "", "", err
	}
	return token, claims.DecisionHash, nil
}

func actionButton(label, style string, value map[string]any) map[string]any {
	button := map[string]any{
		"tag": "button",
		"text": map[string]any{
			"tag":     "plain_text",
			"content": strings.TrimSpace(label),
		},
		"value": value,
	}
	if strings.TrimSpace(style) != "" {
		button["type"] = strings.TrimSpace(style)
	}
	return button
}

func approvalHeaderTitle(gateType string) string {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case string(domain.GateTypeBudget):
		return "Budget Approval Required"
	case string(domain.GateTypeConfirmation):
		return "Confirmation Required"
	case string(domain.GateTypeEvaluation):
		return "Evaluation Decision Required"
	default:
		return "Approval Required"
	}
}

func approvalHeaderTemplate(gateType string) string {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case string(domain.GateTypeBudget):
		return "orange"
	case string(domain.GateTypeConfirmation), string(domain.GateTypeEvaluation):
		return "wathet"
	default:
		return "blue"
	}
}

func approvalMarkdown(approval domain.ApprovalRequestOpenedPayload) string {
	lines := []string{
		fmt.Sprintf("Task: `%s`", strings.TrimSpace(approval.TaskID)),
		fmt.Sprintf("Approval: `%s`", strings.TrimSpace(approval.ApprovalRequestID)),
		fmt.Sprintf("Gate: `%s`", strings.TrimSpace(approval.GateType)),
	}
	if deadline := formatDeadline(approval.DeadlineAt); deadline != "" {
		lines = append(lines, "Deadline: "+deadline)
	}
	if version := strings.TrimSpace(approval.TargetVersionRef); version != "" {
		lines = append(lines, "Target: `"+version+"`")
	}
	return strings.Join(lines, "\n")
}

func waitHeaderTitle(waitingReason string) string {
	switch strings.TrimSpace(waitingReason) {
	case string(domain.WaitingReasonRecovery):
		return "Recovery Action Required"
	case string(domain.WaitingReasonConfirmation):
		return "Confirmation Input Required"
	case string(domain.WaitingReasonBudget):
		return "Budget Input Required"
	default:
		return "Human Input Required"
	}
}

func waitHeaderTemplate(waitingReason string) string {
	switch strings.TrimSpace(waitingReason) {
	case string(domain.WaitingReasonRecovery):
		return "red"
	case string(domain.WaitingReasonBudget):
		return "orange"
	default:
		return "blue"
	}
}

func humanWaitMarkdown(wait domain.HumanWaitRecordedPayload) string {
	lines := []string{
		fmt.Sprintf("Task: `%s`", strings.TrimSpace(wait.TaskID)),
		fmt.Sprintf("Wait: `%s`", strings.TrimSpace(wait.HumanWaitID)),
		fmt.Sprintf("Reason: `%s`", strings.TrimSpace(wait.WaitingReason)),
	}
	if prompt := strings.TrimSpace(wait.PromptRef); prompt != "" {
		lines = append(lines, "Prompt: "+prompt)
	}
	if deadline := formatDeadline(wait.DeadlineAt); deadline != "" {
		lines = append(lines, "Deadline: "+deadline)
	}
	return strings.Join(lines, "\n")
}

func stableDecisionHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func stableNonce(parts ...string) string {
	hash := stableDecisionHash(parts...)
	if len(hash) <= 24 {
		return hash
	}
	return hash[:24]
}

func tokenExpiry(deadline, producedAt time.Time) time.Time {
	if !deadline.IsZero() {
		return deadline.UTC()
	}
	if !producedAt.IsZero() {
		return producedAt.UTC().Add(24 * time.Hour)
	}
	return time.Now().UTC().Add(24 * time.Hour)
}

func formatDeadline(deadline time.Time) string {
	if deadline.IsZero() {
		return ""
	}
	return deadline.UTC().Format(time.RFC3339)
}

func humanizeFieldKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "Input"
	}
	key = strings.ReplaceAll(key, "_", " ")
	key = strings.ReplaceAll(key, "-", " ")
	return strings.ToUpper(key[:1]) + key[1:]
}
