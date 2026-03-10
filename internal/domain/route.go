package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type RouteKeyEncoder interface {
	ReplyTo(eventID string) string
	RepoIssue(repoRef, issueRef string) string
	RepoPR(repoRef, prRef string) string
	Conversation(sourceKind, conversationID, threadID string) string
	ScheduledTask(id string) string
	ControlObject(ref string) string
	WorkflowObject(ref string) string
	Coalescing(sourceKind, actorRef, intentKind, target string, bucket time.Time) string
}

type CanonicalRouteKeyEncoder struct{}

func NewCanonicalRouteKeyEncoder() *CanonicalRouteKeyEncoder {
	return &CanonicalRouteKeyEncoder{}
}

func (e *CanonicalRouteKeyEncoder) ReplyTo(eventID string) string {
	return fmt.Sprintf("reply:%s", strings.TrimSpace(eventID))
}

func (e *CanonicalRouteKeyEncoder) RepoIssue(repoRef, issueRef string) string {
	return fmt.Sprintf("repo_issue:%s:%s", normalizeRepoRef(repoRef), normalizeNumericRef(issueRef))
}

func (e *CanonicalRouteKeyEncoder) RepoPR(repoRef, prRef string) string {
	return fmt.Sprintf("repo_pr:%s:%s", normalizeRepoRef(repoRef), normalizeNumericRef(prRef))
}

func (e *CanonicalRouteKeyEncoder) Conversation(sourceKind, conversationID, threadID string) string {
	thread := strings.TrimSpace(threadID)
	if thread == "" {
		thread = "root"
	}
	return fmt.Sprintf(
		"conversation:%s:%s:%s",
		strings.ToLower(strings.TrimSpace(sourceKind)),
		strings.TrimSpace(conversationID),
		thread,
	)
}

func (e *CanonicalRouteKeyEncoder) ScheduledTask(id string) string {
	return fmt.Sprintf("schedule:%s", strings.TrimSpace(id))
}

func (e *CanonicalRouteKeyEncoder) ControlObject(ref string) string {
	return fmt.Sprintf("control:%s", strings.TrimSpace(ref))
}

func (e *CanonicalRouteKeyEncoder) WorkflowObject(ref string) string {
	return fmt.Sprintf("workflow:%s", strings.TrimSpace(ref))
}

func (e *CanonicalRouteKeyEncoder) Coalescing(sourceKind, actorRef, intentKind, target string, bucket time.Time) string {
	b := bucket.UTC().Truncate(5 * time.Minute).Format(time.RFC3339)
	raw := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(sourceKind)),
		strings.TrimSpace(actorRef),
		strings.TrimSpace(intentKind),
		strings.TrimSpace(target),
		b,
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return "coalesce:" + hex.EncodeToString(sum[:])
}

func normalizeRepoRef(repoRef string) string {
	ref := strings.TrimSpace(strings.ToLower(repoRef))
	ref = strings.TrimPrefix(ref, "https://")
	ref = strings.TrimPrefix(ref, "http://")
	ref = strings.Trim(ref, "/")
	// Convert github.com/org/repo -> github:org/repo as canonical form.
	parts := strings.Split(ref, "/")
	if len(parts) >= 3 && strings.Contains(parts[0], ".") {
		provider := strings.Split(parts[0], ".")[0]
		return fmt.Sprintf("%s:%s/%s", provider, parts[len(parts)-2], parts[len(parts)-1])
	}
	return ref
}

func normalizeNumericRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	n, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		// Fallback for non numeric refs to keep deterministic keying.
		return strings.TrimLeft(ref, "0")
	}
	return strconv.FormatInt(n, 10)
}

type RouteTargetKind string

const (
	RouteTargetNone    RouteTargetKind = ""
	RouteTargetRequest RouteTargetKind = "request"
	RouteTargetTask    RouteTargetKind = "task"
)

type RouteTarget struct {
	Kind RouteTargetKind `json:"kind"`
	ID   string          `json:"id"`
}

func (r RouteTarget) Found() bool {
	return r.Kind != RouteTargetNone && r.ID != ""
}
