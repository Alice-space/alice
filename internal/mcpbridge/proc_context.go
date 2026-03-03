package mcpbridge

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const defaultProcessTreeDepth = 8

// MergeSessionContext fills empty fields in primary with values from fallback.
func MergeSessionContext(primary, fallback SessionContext) SessionContext {
	merged := SessionContext{
		ReceiveIDType:   strings.TrimSpace(primary.ReceiveIDType),
		ReceiveID:       strings.TrimSpace(primary.ReceiveID),
		ResourceRoot:    strings.TrimSpace(primary.ResourceRoot),
		SourceMessageID: strings.TrimSpace(primary.SourceMessageID),
		ActorUserID:     strings.TrimSpace(primary.ActorUserID),
		ActorOpenID:     strings.TrimSpace(primary.ActorOpenID),
		ChatType:        strings.TrimSpace(primary.ChatType),
		SessionKey:      strings.TrimSpace(primary.SessionKey),
	}
	if merged.ReceiveIDType == "" {
		merged.ReceiveIDType = strings.TrimSpace(fallback.ReceiveIDType)
	}
	if merged.ReceiveID == "" {
		merged.ReceiveID = strings.TrimSpace(fallback.ReceiveID)
	}
	if merged.ResourceRoot == "" {
		merged.ResourceRoot = strings.TrimSpace(fallback.ResourceRoot)
	}
	if merged.SourceMessageID == "" {
		merged.SourceMessageID = strings.TrimSpace(fallback.SourceMessageID)
	}
	if merged.ActorUserID == "" {
		merged.ActorUserID = strings.TrimSpace(fallback.ActorUserID)
	}
	if merged.ActorOpenID == "" {
		merged.ActorOpenID = strings.TrimSpace(fallback.ActorOpenID)
	}
	if merged.ChatType == "" {
		merged.ChatType = strings.TrimSpace(fallback.ChatType)
	}
	if merged.SessionKey == "" {
		merged.SessionKey = strings.TrimSpace(fallback.SessionKey)
	}
	return merged
}

// SessionContextFromProcessTree tries to read session context from process
// ancestry (Linux /proc/<pid>/environ), starting from startPID.
func SessionContextFromProcessTree(
	startPID int,
	readFile func(string) ([]byte, error),
	maxDepth int,
) SessionContext {
	if startPID <= 0 || readFile == nil {
		return SessionContext{}
	}
	if maxDepth <= 0 {
		maxDepth = defaultProcessTreeDepth
	}

	pid := startPID
	visited := make(map[int]struct{}, maxDepth)
	for depth := 0; depth < maxDepth && pid > 0; depth++ {
		if _, seen := visited[pid]; seen {
			break
		}
		visited[pid] = struct{}{}

		if envRaw, err := readFile(fmt.Sprintf("/proc/%d/environ", pid)); err == nil {
			candidate := sessionContextFromEnviron(envRaw)
			if candidate.Validate() == nil {
				return candidate
			}
		}

		nextPID, err := readParentPID(readFile, pid)
		if err != nil || nextPID <= 0 || nextPID == pid {
			break
		}
		pid = nextPID
	}
	return SessionContext{}
}

func sessionContextFromEnviron(raw []byte) SessionContext {
	return SessionContext{
		ReceiveIDType:   strings.TrimSpace(readEnvValue(raw, EnvReceiveIDType)),
		ReceiveID:       strings.TrimSpace(readEnvValue(raw, EnvReceiveID)),
		ResourceRoot:    strings.TrimSpace(readEnvValue(raw, EnvResourceRoot)),
		SourceMessageID: strings.TrimSpace(readEnvValue(raw, EnvSourceMessageID)),
		ActorUserID:     strings.TrimSpace(readEnvValue(raw, EnvActorUserID)),
		ActorOpenID:     strings.TrimSpace(readEnvValue(raw, EnvActorOpenID)),
		ChatType:        strings.TrimSpace(readEnvValue(raw, EnvChatType)),
		SessionKey:      strings.TrimSpace(readEnvValue(raw, EnvSessionKey)),
	}
}

func readEnvValue(raw []byte, key string) string {
	if len(raw) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	prefix := []byte(key + "=")
	entries := bytes.Split(raw, []byte{0})
	for _, entry := range entries {
		if len(entry) == 0 || !bytes.HasPrefix(entry, prefix) {
			continue
		}
		return string(entry[len(prefix):])
	}
	return ""
}

func readParentPID(readFile func(string) ([]byte, error), pid int) (int, error) {
	raw, err := readFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "PPid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("invalid ppid line: %q", line)
		}
		parentPID, convErr := strconv.Atoi(fields[1])
		if convErr != nil {
			return 0, convErr
		}
		return parentPID, nil
	}
	return 0, errors.New("ppid not found in process status")
}
