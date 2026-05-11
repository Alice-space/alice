package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

var (
	pendingQuestions   sync.Map
	questionHTTPClient = &http.Client{}
)

func RegisterPendingQuestion(requestID, baseURL string) {
	requestID = strings.TrimSpace(requestID)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if requestID == "" || baseURL == "" {
		return
	}
	pendingQuestions.Store(requestID, baseURL)
}

func LookupPendingQuestion(requestID string) (string, bool) {
	requestID = strings.TrimSpace(requestID)
	raw, ok := pendingQuestions.Load(requestID)
	if !ok {
		return "", false
	}
	baseURL, _ := raw.(string)
	return baseURL, true
}

func RemovePendingQuestion(requestID string) {
	pendingQuestions.Delete(strings.TrimSpace(requestID))
}

func ReplyQuestion(ctx context.Context, requestID string, answers [][]string) error {
	requestID = strings.TrimSpace(requestID)
	baseURL, ok := LookupPendingQuestion(requestID)
	if !ok {
		return fmt.Errorf("unknown question requestID: %s", requestID)
	}
	body := map[string]any{"answers": answers}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/question/"+requestID+"/reply", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := questionHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("reply question failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	RemovePendingQuestion(requestID)
	return nil
}

func RejectQuestion(ctx context.Context, requestID string) error {
	requestID = strings.TrimSpace(requestID)
	baseURL, ok := LookupPendingQuestion(requestID)
	if !ok {
		return fmt.Errorf("unknown question requestID: %s", requestID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/question/"+requestID+"/reject", nil)
	if err != nil {
		return err
	}
	resp, err := questionHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("reject question failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	RemovePendingQuestion(requestID)
	return nil
}
