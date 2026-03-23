package codex

import (
	"path/filepath"
	"sort"
	"strings"
)

func parseFileChangeMessage(item map[string]any) string {
	if item == nil {
		return ""
	}

	entries := collectFileChangeEntries(item)
	if len(entries) == 0 {
		return ""
	}

	messages := make([]string, 0, len(entries))
	for _, entry := range entries {
		message := formatFileChangeMessageWithStatus(entry.Path, entry.Status, entry.Stat)
		if strings.TrimSpace(message) == "" {
			continue
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "\n")
}

type fileChangeEntry struct {
	Path     string
	Status   fileChangeStatus
	Stat     fileDiffStat
	HasStats bool
}

func collectFileChangePaths(item map[string]any) []string {
	if item == nil {
		return nil
	}

	seen := make(map[string]struct{}, 4)
	addPath := func(raw string) {
		path := strings.TrimSpace(raw)
		if path == "" {
			return
		}
		seen[path] = struct{}{}
	}

	addPath(extractString(item, "path", "file_path", "filename", "file"))
	if changed, ok := item["changed_file"].(map[string]any); ok {
		addPath(extractString(changed, "path", "file_path", "filename", "file"))
	}
	if changes, ok := item["changes"].([]any); ok {
		for _, change := range changes {
			entry, ok := change.(map[string]any)
			if !ok {
				continue
			}
			addPath(extractString(entry, "path", "file_path", "filename", "file"))
		}
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func collectFileChangeEntries(item map[string]any) []fileChangeEntry {
	if item == nil {
		return nil
	}

	defaultStatus := extractFileChangeStatus(item)
	defaultStat, defaultHasStats := extractDiffStat(item)
	entriesByPath := make(map[string]fileChangeEntry, 4)

	mergeEntry := func(rawPath string, status fileChangeStatus, stat fileDiffStat, hasStats bool) {
		path := normalizeFileChangePath(rawPath)
		if path == "" {
			return
		}
		if status == fileChangeStatusUnknown {
			status = defaultStatus
		}
		if status == fileChangeStatusUnknown {
			status = fileChangeStatusModified
		}
		if !hasStats && defaultHasStats {
			stat = defaultStat
			hasStats = true
		}

		incoming := fileChangeEntry{Path: path, Status: status, Stat: stat, HasStats: hasStats}
		existing, exists := entriesByPath[path]
		if !exists {
			entriesByPath[path] = incoming
			return
		}

		if existing.Status == fileChangeStatusUnknown {
			existing.Status = incoming.Status
		} else if existing.Status == fileChangeStatusModified &&
			(incoming.Status == fileChangeStatusAdded || incoming.Status == fileChangeStatusDeleted) {
			existing.Status = incoming.Status
		}

		existingHasNumbers := existing.Stat.Additions != 0 || existing.Stat.Deletions != 0
		incomingHasNumbers := incoming.Stat.Additions != 0 || incoming.Stat.Deletions != 0
		if !existing.HasStats && incoming.HasStats {
			existing.Stat = incoming.Stat
			existing.HasStats = true
		} else if incoming.HasStats && incomingHasNumbers && !existingHasNumbers {
			existing.Stat = incoming.Stat
			existing.HasStats = true
		}

		entriesByPath[path] = existing
	}

	mergeEntry(extractString(item, "path", "file_path", "filename", "file"), defaultStatus, defaultStat, defaultHasStats)

	if changed, ok := item["changed_file"].(map[string]any); ok {
		status := extractFileChangeStatus(changed)
		stat, hasStats := extractDiffStat(changed)
		mergeEntry(extractString(changed, "path", "file_path", "filename", "file"), status, stat, hasStats)
	}

	if changes, ok := item["changes"].([]any); ok {
		for _, change := range changes {
			entry, ok := change.(map[string]any)
			if !ok {
				continue
			}
			status := extractFileChangeStatus(entry)
			stat, hasStats := extractDiffStat(entry)
			mergeEntry(extractString(entry, "path", "file_path", "filename", "file"), status, stat, hasStats)
		}
	}

	if len(entriesByPath) == 0 {
		for _, path := range collectFileChangePaths(item) {
			mergeEntry(path, defaultStatus, defaultStat, defaultHasStats)
		}
	}

	paths := make([]string, 0, len(entriesByPath))
	for path := range entriesByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	entries := make([]fileChangeEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, entriesByPath[path])
	}
	return entries
}

func extractDiffStat(payload map[string]any) (fileDiffStat, bool) {
	if payload == nil {
		return fileDiffStat{}, false
	}

	additions, hasAdditions := extractIntWithPresence(payload, "added_lines", "additions", "added", "insertions", "plus")
	deletions, hasDeletions := extractIntWithPresence(payload, "removed_lines", "deletions", "removed", "minus")

	if stats, ok := payload["diff_stats"].(map[string]any); ok {
		if !hasAdditions {
			if value, found := extractIntWithPresence(stats, "added_lines", "additions", "added", "insertions", "plus"); found {
				additions = value
				hasAdditions = true
			}
		}
		if !hasDeletions {
			if value, found := extractIntWithPresence(stats, "removed_lines", "deletions", "removed", "minus"); found {
				deletions = value
				hasDeletions = true
			}
		}
	}

	if stats, ok := payload["stats"].(map[string]any); ok {
		if !hasAdditions {
			if value, found := extractIntWithPresence(stats, "added_lines", "additions", "added", "insertions", "plus"); found {
				additions = value
				hasAdditions = true
			}
		}
		if !hasDeletions {
			if value, found := extractIntWithPresence(stats, "removed_lines", "deletions", "removed", "minus"); found {
				deletions = value
				hasDeletions = true
			}
		}
	}

	return fileDiffStat{Additions: additions, Deletions: deletions}, hasAdditions || hasDeletions
}

func extractFileChangeStatus(payload map[string]any) fileChangeStatus {
	status := extractString(
		payload,
		"kind",
		"change_type",
		"change_kind",
		"change",
		"operation",
		"op",
		"action",
		"file_change_type",
		"status",
	)
	return normalizeFileChangeStatus(status)
}

func normalizeFileChangePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(path)
	path = strings.TrimPrefix(path, "./")
	const aliceRepoPrefix = "/home/codexbot/alice/"
	path = strings.TrimPrefix(path, aliceRepoPrefix)
	return strings.TrimSpace(path)
}
