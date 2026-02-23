package connector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gitee.com/alicespace/alice/internal/logging"
)

type runtimeStateSnapshot struct {
	Latest      map[string]uint64             `json:"latest"`
	Pending     []Job                         `json:"pending"`
	MediaWindow map[string][]mediaWindowEntry `json:"media_window,omitempty"`
}

func (a *App) LoadRuntimeState(path string) error {
	path = strings.TrimSpace(path)

	a.mu.Lock()
	a.runtimeStatePath = path
	a.mu.Unlock()

	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read runtime state failed: %w", err)
	}

	var snapshot runtimeStateSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("parse runtime state failed: %w", err)
	}

	loadedLatest := make(map[string]uint64, len(snapshot.Latest))
	for rawSessionKey, version := range snapshot.Latest {
		sessionKey := strings.TrimSpace(rawSessionKey)
		if sessionKey == "" || version == 0 {
			continue
		}
		if version > loadedLatest[sessionKey] {
			loadedLatest[sessionKey] = version
		}
	}

	pendingByKey := make(map[string]Job, len(snapshot.Pending))
	for _, rawJob := range snapshot.Pending {
		job, ok := normalizeRuntimeJob(rawJob)
		if !ok {
			continue
		}
		if job.SessionVersion > loadedLatest[job.SessionKey] {
			loadedLatest[job.SessionKey] = job.SessionVersion
		}
		pendingByKey[pendingJobKey(job)] = job
	}

	now := a.now()
	loadedMediaWindow := make(map[string][]mediaWindowEntry, len(snapshot.MediaWindow))
	for rawKey, rawEntries := range snapshot.MediaWindow {
		windowKey := strings.TrimSpace(rawKey)
		if windowKey == "" {
			continue
		}
		entries := make([]mediaWindowEntry, 0, len(rawEntries))
		for _, rawEntry := range rawEntries {
			entry, ok := normalizeMediaWindowEntry(rawEntry)
			if !ok {
				continue
			}
			if !entry.ReceivedAt.IsZero() && now.Sub(entry.ReceivedAt) > a.groupContextWindowTTL() {
				continue
			}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].ReceivedAt.Before(entries[j].ReceivedAt)
		})
		if len(entries) > maxMediaWindowEntries {
			entries = append([]mediaWindowEntry(nil), entries[len(entries)-maxMediaWindowEntries:]...)
		}
		loadedMediaWindow[windowKey] = entries
	}

	restoredJobs := make([]Job, 0, len(pendingByKey))
	for _, job := range pendingByKey {
		restoredJobs = append(restoredJobs, job)
	}
	sortPendingJobs(restoredJobs)

	a.mu.Lock()
	a.latest = loadedLatest
	a.pending = pendingByKey
	a.mediaWindow = loadedMediaWindow
	a.runtimeStateVersion = 0
	a.runtimeStateFlushedVersion = 0
	a.mu.Unlock()

	restoredCount := 0
	droppedCount := 0
	for _, job := range restoredJobs {
		select {
		case a.queue <- job:
			restoredCount++
		default:
			droppedCount++
			a.completePendingJob(job)
		}
	}

	logging.Debugf(
		"runtime state loaded file=%s latest=%d pending=%d media_windows=%d restored=%d dropped=%d",
		path,
		len(loadedLatest),
		len(pendingByKey),
		len(loadedMediaWindow),
		restoredCount,
		droppedCount,
	)
	return nil
}

func (a *App) FlushRuntimeState() error {
	return a.flushRuntimeStateFile(true)
}

func (a *App) FlushRuntimeStateIfDirty() error {
	return a.flushRuntimeStateFile(false)
}

func (a *App) flushRuntimeStateFile(force bool) error {
	a.mu.Lock()
	a.pruneExpiredMediaWindowLocked(a.now())
	path := strings.TrimSpace(a.runtimeStatePath)
	currentVersion := a.runtimeStateVersion
	flushedVersion := a.runtimeStateFlushedVersion
	if !force && currentVersion == flushedVersion {
		a.mu.Unlock()
		return nil
	}
	if path == "" {
		a.mu.Unlock()
		return nil
	}

	snapshot := runtimeStateSnapshot{
		Latest:      make(map[string]uint64, len(a.latest)),
		Pending:     make([]Job, 0, len(a.pending)),
		MediaWindow: make(map[string][]mediaWindowEntry, len(a.mediaWindow)),
	}
	for sessionKey, version := range a.latest {
		if strings.TrimSpace(sessionKey) == "" || version == 0 {
			continue
		}
		snapshot.Latest[sessionKey] = version
	}
	for _, job := range a.pending {
		snapshot.Pending = append(snapshot.Pending, job)
	}
	for windowKey, entries := range a.mediaWindow {
		if strings.TrimSpace(windowKey) == "" || len(entries) == 0 {
			continue
		}
		clonedEntries := make([]mediaWindowEntry, 0, len(entries))
		for _, entry := range entries {
			normalized, ok := normalizeMediaWindowEntry(entry)
			if !ok {
				continue
			}
			clonedEntries = append(clonedEntries, normalized)
		}
		if len(clonedEntries) == 0 {
			continue
		}
		snapshot.MediaWindow[windowKey] = clonedEntries
	}
	a.mu.Unlock()

	sortPendingJobs(snapshot.Pending)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create runtime state dir failed: %w", err)
	}

	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal runtime state failed: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".runtime_state.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp runtime state failed: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp runtime state failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp runtime state failed: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace runtime state failed: %w", err)
	}

	a.mu.Lock()
	if currentVersion > a.runtimeStateFlushedVersion {
		a.runtimeStateFlushedVersion = currentVersion
	}
	a.mu.Unlock()
	return nil
}

func (a *App) completePendingJob(job Job) {
	key := pendingJobKey(job)
	if key == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.pending[key]; !ok {
		return
	}
	delete(a.pending, key)
	a.markRuntimeStateChangedLocked()
}

func (a *App) rememberPendingJobLocked(job Job) {
	normalized, ok := normalizeRuntimeJob(job)
	if !ok {
		return
	}
	key := pendingJobKey(normalized)
	if key == "" {
		return
	}
	a.pending[key] = normalized
	a.markRuntimeStateChangedLocked()
}

func (a *App) updatePendingJobWorkflowPhase(job Job, phase string) {
	key := pendingJobKey(job)
	if key == "" {
		return
	}
	normalizedPhase := normalizeJobWorkflowPhase(phase)

	a.mu.Lock()
	defer a.mu.Unlock()

	pendingJob, ok := a.pending[key]
	if !ok {
		pendingJob = job
	}
	pendingJob.WorkflowPhase = normalizedPhase

	normalizedJob, normalized := normalizeRuntimeJob(pendingJob)
	if !normalized {
		return
	}
	if existing, ok := a.pending[key]; ok && normalizeJobWorkflowPhase(existing.WorkflowPhase) == normalizedJob.WorkflowPhase {
		return
	}

	a.pending[key] = normalizedJob
	a.markRuntimeStateChangedLocked()
}

func (a *App) removeOlderPendingJobsLocked(sessionKey string, keepVersion uint64) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || keepVersion == 0 {
		return
	}
	changed := false
	for key, job := range a.pending {
		if strings.TrimSpace(job.SessionKey) != sessionKey {
			continue
		}
		if job.SessionVersion >= keepVersion {
			continue
		}
		delete(a.pending, key)
		changed = true
	}
	if changed {
		a.markRuntimeStateChangedLocked()
	}
}

func (a *App) removePendingBySessionVersionLocked(sessionKey string, sessionVersion uint64) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || sessionVersion == 0 {
		return
	}
	changed := false
	for key, job := range a.pending {
		if strings.TrimSpace(job.SessionKey) != sessionKey {
			continue
		}
		if job.SessionVersion != sessionVersion {
			continue
		}
		delete(a.pending, key)
		changed = true
	}
	if changed {
		a.markRuntimeStateChangedLocked()
	}
}

func (a *App) markRuntimeStateChangedLocked() {
	a.runtimeStateVersion++
}

func pendingJobKey(job Job) string {
	eventID := strings.TrimSpace(job.EventID)
	if eventID != "" {
		return "event:" + eventID
	}
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey == "" {
		sessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if sessionKey == "" || job.SessionVersion == 0 {
		return ""
	}
	return fmt.Sprintf("session:%s#%d", sessionKey, job.SessionVersion)
}

func normalizeRuntimeJob(job Job) (Job, bool) {
	job.ReceiveID = strings.TrimSpace(job.ReceiveID)
	job.ReceiveIDType = strings.TrimSpace(job.ReceiveIDType)
	job.ChatType = strings.TrimSpace(job.ChatType)
	job.SenderOpenID = strings.TrimSpace(job.SenderOpenID)
	job.SenderUserID = strings.TrimSpace(job.SenderUserID)
	job.SourceMessageID = strings.TrimSpace(job.SourceMessageID)
	job.ReplyParentMessageID = strings.TrimSpace(job.ReplyParentMessageID)
	job.ThreadID = strings.TrimSpace(job.ThreadID)
	job.RootID = strings.TrimSpace(job.RootID)
	job.MessageType = strings.TrimSpace(job.MessageType)
	job.RawContent = strings.TrimSpace(job.RawContent)
	job.EventID = strings.TrimSpace(job.EventID)
	job.SessionKey = strings.TrimSpace(job.SessionKey)
	job.WorkflowPhase = normalizeJobWorkflowPhase(job.WorkflowPhase)
	if len(job.Attachments) > 0 {
		normalized := make([]Attachment, 0, len(job.Attachments))
		for _, rawAttachment := range job.Attachments {
			attachment, ok := normalizeAttachment(rawAttachment)
			if !ok {
				continue
			}
			normalized = append(normalized, attachment)
		}
		job.Attachments = normalized
	}
	if job.SessionKey == "" {
		job.SessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if job.SessionKey == "" || job.SessionVersion == 0 {
		return Job{}, false
	}
	return job, true
}

func normalizeMediaWindowEntry(entry mediaWindowEntry) (mediaWindowEntry, bool) {
	entry.SourceMessageID = strings.TrimSpace(entry.SourceMessageID)
	entry.MessageType = strings.TrimSpace(entry.MessageType)
	entry.Speaker = strings.TrimSpace(entry.Speaker)
	entry.Text = strings.TrimSpace(entry.Text)
	entry.RawContent = strings.TrimSpace(entry.RawContent)
	normalized := make([]Attachment, 0, len(entry.Attachments))
	for _, rawAttachment := range entry.Attachments {
		attachment, ok := normalizeAttachment(rawAttachment)
		if !ok {
			continue
		}
		normalized = append(normalized, attachment)
	}
	entry.Attachments = normalized
	if !hasMediaWindowEntryContent(entry) {
		return mediaWindowEntry{}, false
	}
	if entry.ReceivedAt.IsZero() {
		entry.ReceivedAt = time.Now()
	}
	return entry, true
}

func normalizeAttachment(attachment Attachment) (Attachment, bool) {
	attachment.SourceMessageID = strings.TrimSpace(attachment.SourceMessageID)
	attachment.Kind = strings.TrimSpace(attachment.Kind)
	attachment.FileKey = strings.TrimSpace(attachment.FileKey)
	attachment.ImageKey = strings.TrimSpace(attachment.ImageKey)
	attachment.FileName = strings.TrimSpace(attachment.FileName)
	attachment.LocalPath = strings.TrimSpace(attachment.LocalPath)
	attachment.DownloadError = strings.TrimSpace(attachment.DownloadError)
	if attachment.Kind == "" &&
		attachment.FileKey == "" &&
		attachment.ImageKey == "" &&
		attachment.FileName == "" &&
		attachment.LocalPath == "" &&
		attachment.DownloadError == "" {
		return Attachment{}, false
	}
	return attachment, true
}

func sortPendingJobs(jobs []Job) {
	sort.Slice(jobs, func(i, j int) bool {
		left := jobs[i]
		right := jobs[j]

		if !left.ReceivedAt.Equal(right.ReceivedAt) {
			if left.ReceivedAt.IsZero() {
				return false
			}
			if right.ReceivedAt.IsZero() {
				return true
			}
			return left.ReceivedAt.Before(right.ReceivedAt)
		}
		if left.SessionKey != right.SessionKey {
			return left.SessionKey < right.SessionKey
		}
		if left.SessionVersion != right.SessionVersion {
			return left.SessionVersion < right.SessionVersion
		}
		return left.EventID < right.EventID
	})
}
