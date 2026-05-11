package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	llm "github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
)

var (
	pendingQuestionStates sync.Map
)

func storeQuestionState(requestID string, qs questionCardState) {
	pendingQuestionStates.Store(requestID, qs)
}

func loadQuestionState(requestID string) (questionCardState, bool) {
	raw, ok := pendingQuestionStates.Load(requestID)
	if !ok {
		return questionCardState{}, false
	}
	qs, _ := raw.(questionCardState)
	return qs, true
}

type questionCardState struct {
	RequestID string             `json:"request_id"`
	Questions []llm.QuestionInfo `json:"questions"`
}

func buildQuestionCard(requestID string, questions []llm.QuestionInfo) string {
	if len(questions) == 0 {
		return ""
	}
	elements := []any{
		map[string]any{
			"tag":     "markdown",
			"content": "以下问题需要你的回答：",
		},
	}

	formElements := make([]any, 0, len(questions)*3+1)
	for i, q := range questions {
		name := fmt.Sprintf("q%d", i)
		formElements = append(formElements, buildQuestionMarkdown(i+1, q))
		formElements = append(formElements, buildQuestionSelect(name, q))
		if i < len(questions)-1 {
			formElements = append(formElements, map[string]any{"tag": "hr"})
		}
	}
	formElements = append(formElements, map[string]any{
		"tag": "button",
		"text": map[string]any{
			"tag":     "lark_md",
			"content": "**提交答案**",
		},
		"type": "primary",
		"value": map[string]any{
			"alice_action":   "question",
			"request_id":     requestID,
			"question_count": fmt.Sprintf("%d", len(questions)),
		},
	})

	elements = append(elements, map[string]any{
		"tag":      "form",
		"name":     "alice_questions",
		"elements": formElements,
	})

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": false,
			"update_multi":   false,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": questionCardTitle(questions),
			},
			"template": "blue",
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return ""
	}
	return string(raw)
}

func questionCardTitle(questions []llm.QuestionInfo) string {
	if len(questions) == 1 {
		header := strings.TrimSpace(questions[0].Header)
		if header != "" {
			if len(header) > 30 {
				header = header[:30]
			}
			return header
		}
		return "请回答以下问题"
	}
	return fmt.Sprintf("请回答 %d 个问题", len(questions))
}

func buildQuestionMarkdown(index int, q llm.QuestionInfo) map[string]any {
	text := strings.TrimSpace(q.Question)
	if text == "" {
		text = strings.TrimSpace(q.Header)
	}
	if text == "" {
		text = fmt.Sprintf("问题 %d", index)
	}
	if q.Multiple {
		text = fmt.Sprintf("**%d. %s** (多选)", index, text)
	} else {
		text = fmt.Sprintf("**%d. %s**", index, text)
	}
	return map[string]any{
		"tag":     "lark_md",
		"content": text,
	}
}

func buildQuestionSelect(name string, q llm.QuestionInfo) map[string]any {
	options := make([]any, 0, len(q.Options))
	for _, opt := range q.Options {
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			continue
		}
		options = append(options, map[string]any{
			"text": map[string]any{
				"tag":     "plain_text",
				"content": label,
			},
			"value": label,
		})
	}
	placeholder := "请选择..."
	if len(options) == 0 {
		return map[string]any{
			"tag":     "markdown",
			"content": fmt.Sprintf("(无可用选项)"),
		}
	}
	tag := "select_static"
	if q.Multiple {
		tag = "multi_select_static"
	}
	return map[string]any{
		"tag":         tag,
		"name":        name,
		"placeholder": map[string]any{"tag": "plain_text", "content": placeholder},
		"options":     options,
	}
}

func parseQuestionAnswers(formValue map[string]any, questions []llm.QuestionInfo) ([][]string, error) {
	answers := make([][]string, len(questions))
	for i, q := range questions {
		name := fmt.Sprintf("q%d", i)
		raw, ok := formValue[name]
		if !ok || raw == nil {
			if q.Multiple {
				answers[i] = []string{}
			} else {
				return nil, fmt.Errorf("问题 %d (%q) 未回答", i+1, q.Question)
			}
			continue
		}
		if q.Multiple {
			labels := formValueToStringSlice(raw)
			answers[i] = labels
		} else {
			label := formValueToString(raw)
			if label == "" {
				return nil, fmt.Errorf("问题 %d (%q) 未回答", i+1, q.Question)
			}
			answers[i] = []string{label}
		}
	}
	return answers, nil
}

func formValueToString(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimSpace(fmt.Sprint(v))
	case json.Number:
		return v.String()
	default:
		if raw != nil {
			rawJSON, err := json.Marshal(raw)
			if err == nil {
				s := strings.TrimSpace(string(rawJSON))
				s = strings.Trim(s, `"`)
				return s
			}
		}
		return ""
	}
}

func formValueToStringSlice(raw any) []string {
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := formValueToString(item)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		if strings.HasPrefix(s, "[") {
			var arr []string
			if err := json.Unmarshal([]byte(s), &arr); err == nil {
				result := make([]string, 0, len(arr))
				for _, item := range arr {
					item = strings.TrimSpace(item)
					if item != "" {
						result = append(result, item)
					}
				}
				return result
			}
		}
		return []string{s}
	case float64:
		return []string{fmt.Sprint(v)}
	default:
		return nil
	}
}

func (p *Processor) sendQuestionCard(ctx context.Context, job Job, rawJSON string) {
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		return
	}
	var tq llm.TurnQuestion
	if err := json.Unmarshal([]byte(rawJSON), &tq); err != nil {
		logging.Warnf("parse question card json failed: %v", err)
		return
	}
	requestID := strings.TrimSpace(tq.RequestID)
	if requestID == "" || len(tq.Questions) == 0 {
		return
	}
	storeQuestionState(requestID, questionCardState{
		RequestID: requestID,
		Questions: tq.Questions,
	})
	cardContent := buildQuestionCard(requestID, tq.Questions)
	if cardContent == "" {
		return
	}
	if _, err := p.replies.replyCard(ctx, job.SourceMessageID, cardContent, jobPrefersThreadReply(job)); err != nil {
		logging.Warnf("send question card failed event_id=%s: %v", job.EventID, err)
	}
}

var questionCardPatchedContent = `{"schema":"2.0","config":{"enable_forward":false,"update_multi":false},"header":{"title":{"tag":"plain_text","content":"已收到回答"},"template":"green"},"body":{"elements":[{"tag":"markdown","content":"你的答案已提交，AI 将继续处理。"}]}}`

func (a *App) patchQuestionCardAnswered(ctx context.Context, openMessageID string) {
	openMessageID = strings.TrimSpace(openMessageID)
	if openMessageID == "" || a == nil || a.processor == nil || a.processor.sender == nil {
		return
	}
	patcher, ok := a.processor.sender.(cardPatcher)
	if !ok {
		return
	}
	if err := patcher.PatchCard(ctx, openMessageID, questionCardPatchedContent); err != nil {
		logging.Warnf("patch question card answered failed message_id=%s: %v", openMessageID, err)
	}
}
