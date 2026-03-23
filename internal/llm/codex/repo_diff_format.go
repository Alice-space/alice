package codex

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func formatFileChangeMessage(path string, stat fileDiffStat) string {
	return formatFileChangeMessageWithStatus(path, fileChangeStatusModified, stat)
}

func formatFileChangeMessageWithStatus(path string, status fileChangeStatus, stat fileDiffStat) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	label := fileChangeStatusLabel(status)
	if stat.Additions == 0 && stat.Deletions == 0 {
		return fmt.Sprintf("- `%s` %s", path, label)
	}
	return fmt.Sprintf("- `%s` %s (+%d/-%d)", path, label, stat.Additions, stat.Deletions)
}

func fileChangeStatusLabel(status fileChangeStatus) string {
	switch status {
	case fileChangeStatusAdded:
		return "已新增"
	case fileChangeStatusDeleted:
		return "已删除"
	default:
		return "已更改"
	}
}

func normalizeFileChangeStatus(raw string) fileChangeStatus {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return fileChangeStatusUnknown
	}

	switch normalized {
	case "已更改", "更改", "修改", "updated", "update", "modified", "modify", "changed", "change", "edit", "edited", "rewrite", "rewritten":
		return fileChangeStatusModified
	case "已新增", "新增", "新建", "create", "created", "add", "added", "new", "created_file":
		return fileChangeStatusAdded
	case "已删除", "删除", "delete", "deleted", "remove", "removed", "rm", "unlink":
		return fileChangeStatusDeleted
	default:
		return fileChangeStatusUnknown
	}
}

func enrichFileChangeMessageStats(ctx context.Context, message string, repos []string) string {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	if len(lines) == 0 {
		return ""
	}

	updated := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		parsedLine, ok := parseFormattedFileChangeLine(line)
		if !ok {
			updated = append(updated, line)
			continue
		}

		status := parsedLine.Status
		if status == fileChangeStatusUnknown {
			status = fileChangeStatusModified
		}
		stat := parsedLine.Stat
		hasNumbers := stat.Additions != 0 || stat.Deletions != 0

		resolvedStatus, resolvedStat, resolved := resolveFileChangeInfoByGitDiff(ctx, repos, parsedLine.Path)
		if resolved {
			if status == fileChangeStatusModified &&
				(resolvedStatus == fileChangeStatusAdded || resolvedStatus == fileChangeStatusDeleted) {
				status = resolvedStatus
			}
			if !hasNumbers && (resolvedStat.Additions != 0 || resolvedStat.Deletions != 0) {
				stat = resolvedStat
				hasNumbers = true
			}
			if status == fileChangeStatusUnknown {
				status = resolvedStatus
			}
		}
		if status == fileChangeStatusUnknown {
			status = fileChangeStatusModified
		}

		if !hasNumbers {
			stat = fileDiffStat{}
		}
		updated = append(updated, formatFileChangeMessageWithStatus(parsedLine.Path, status, stat))
	}

	return strings.Join(updated, "\n")
}

func parseFormattedFileChangeLine(line string) (parsedFileChangeLine, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return parsedFileChangeLine{}, false
	}
	return parseMarkdownFileChangeLine(line)
}

func parseMarkdownFileChangeLine(line string) (parsedFileChangeLine, bool) {
	const prefix = "- `"
	if !strings.HasPrefix(line, prefix) {
		return parsedFileChangeLine{}, false
	}
	rest := strings.TrimPrefix(line, prefix)
	endPath := strings.Index(rest, "`")
	if endPath <= 0 {
		return parsedFileChangeLine{}, false
	}
	path := strings.TrimSpace(rest[:endPath])
	if path == "" {
		return parsedFileChangeLine{}, false
	}
	tail := strings.TrimSpace(rest[endPath+1:])
	if tail == "" {
		return parsedFileChangeLine{}, false
	}

	statusPart := tail
	stat := fileDiffStat{}
	hasStats := false
	if idx := strings.Index(tail, " (+"); idx >= 0 && strings.HasSuffix(tail, ")") {
		statusPart = strings.TrimSpace(tail[:idx])
		statsPart := strings.TrimSpace(tail[idx+2 : len(tail)-1])
		additions, deletions, ok := parseMarkdownStats(statsPart)
		if ok {
			stat = fileDiffStat{Additions: additions, Deletions: deletions}
			hasStats = true
		}
	}

	status := normalizeFileChangeStatus(statusPart)
	if status == fileChangeStatusUnknown {
		return parsedFileChangeLine{}, false
	}

	return parsedFileChangeLine{
		Path:     path,
		Status:   status,
		Stat:     stat,
		HasStats: hasStats,
	}, true
}

func parseMarkdownStats(statsPart string) (additions int, deletions int, ok bool) {
	statsPart = strings.TrimSpace(statsPart)
	if !strings.HasPrefix(statsPart, "+") {
		return 0, 0, false
	}
	trimmed := strings.TrimPrefix(statsPart, "+")
	parts := strings.SplitN(trimmed, "/-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	add, addErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	del, delErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if addErr != nil || delErr != nil {
		return 0, 0, false
	}
	return add, del, true
}
