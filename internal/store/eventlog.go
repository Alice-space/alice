package store

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"alice/internal/domain"
)

const (
	defaultSegmentBytes = int64(128 * 1024 * 1024)
	defaultRollEvery    = time.Hour
)

type EventStore interface {
	Append(ctx context.Context, batch []domain.EventEnvelope) error
	Replay(ctx context.Context, fromHLC string, fn func(domain.EventEnvelope) error) error
}

type JSONLEventLog struct {
	root          string
	maxSegment    int64
	rollEvery     time.Duration
	mu            sync.Mutex
	currentFile   *os.File
	currentIndex  uint64
	segmentOpened time.Time
}

func OpenJSONLEventLog(root string, maxSegmentBytes int64, rollEvery time.Duration) (*JSONLEventLog, error) {
	if maxSegmentBytes <= 0 {
		maxSegmentBytes = defaultSegmentBytes
	}
	if rollEvery <= 0 {
		rollEvery = defaultRollEvery
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create eventlog dir: %w", err)
	}
	log := &JSONLEventLog{root: root, maxSegment: maxSegmentBytes, rollEvery: rollEvery}
	if err := log.ensureCurrentSegmentLocked(); err != nil {
		return nil, err
	}
	return log, nil
}

func (l *JSONLEventLog) Append(_ context.Context, batch []domain.EventEnvelope) error {
	if len(batch) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.rotateIfNeededLocked(); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, evt := range batch {
		if err := enc.Encode(evt); err != nil {
			return fmt.Errorf("encode event %s: %w", evt.EventID, err)
		}
	}
	if _, err := l.currentFile.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("append batch: %w", err)
	}
	if err := l.currentFile.Sync(); err != nil {
		return fmt.Errorf("sync eventlog: %w", err)
	}
	return nil
}

func (l *JSONLEventLog) Replay(ctx context.Context, fromHLC string, fn func(domain.EventEnvelope) error) error {
	segments, err := l.segments()
	if err != nil {
		return err
	}
	for _, seg := range segments {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := replaySegment(seg, fromHLC, fn); err != nil {
			return fmt.Errorf("replay %s: %w", seg, err)
		}
	}
	return nil
}

func (l *JSONLEventLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.currentFile == nil {
		return nil
	}
	return l.currentFile.Close()
}

func (l *JSONLEventLog) rotateIfNeededLocked() error {
	if l.currentFile == nil {
		return l.ensureCurrentSegmentLocked()
	}
	info, err := l.currentFile.Stat()
	if err != nil {
		return fmt.Errorf("stat current segment: %w", err)
	}
	if info.Size() < l.maxSegment && time.Since(l.segmentOpened) < l.rollEvery {
		return nil
	}
	if err := l.currentFile.Close(); err != nil {
		return fmt.Errorf("close segment: %w", err)
	}
	l.currentFile = nil
	l.currentIndex++
	return l.ensureCurrentSegmentLocked()
}

func (l *JSONLEventLog) ensureCurrentSegmentLocked() error {
	if l.currentFile != nil {
		return nil
	}
	segments, err := l.segments()
	if err != nil {
		return err
	}
	if len(segments) > 0 {
		last := filepath.Base(segments[len(segments)-1])
		idx, parseErr := parseSegmentIndex(last)
		if parseErr == nil {
			l.currentIndex = idx
		}
	}
	if len(segments) == 0 {
		l.currentIndex = 1
	}
	segmentPath := filepath.Join(l.root, fmt.Sprintf("%012d.jsonl", l.currentIndex))
	f, err := os.OpenFile(segmentPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open segment %s: %w", segmentPath, err)
	}
	l.currentFile = f
	l.segmentOpened = time.Now().UTC()
	return nil
}

func (l *JSONLEventLog) segments() ([]string, error) {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return nil, fmt.Errorf("list eventlog segments: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") {
			out = append(out, filepath.Join(l.root, name))
		}
	}
	sort.Strings(out)
	return out, nil
}

func replaySegment(path, fromHLC string, fn func(domain.EventEnvelope) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var evt domain.EventEnvelope
			if umErr := json.Unmarshal(line, &evt); umErr != nil {
				return fmt.Errorf("unmarshal line: %w", umErr)
			}
			if fromHLC != "" && evt.GlobalHLC <= fromHLC {
				// fromHLC is exclusive.
			} else {
				if applyErr := fn(evt); applyErr != nil {
					return applyErr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func parseSegmentIndex(name string) (uint64, error) {
	base := strings.TrimSuffix(name, ".jsonl")
	return strconv.ParseUint(base, 10, 64)
}
