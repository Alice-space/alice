package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	cron "github.com/robfig/cron/v3"
)

func ComputeFireID(scheduledTaskID string, scheduledForWindow time.Time) string {
	raw := fmt.Sprintf("%s:%s", scheduledTaskID, scheduledForWindow.UTC().Format(time.RFC3339))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func NextCronFire(specText, timezone string, after time.Time) (time.Time, error) {
	if specText == "" {
		return time.Time{}, fmt.Errorf("schedule spec_text is required")
	}
	loc := time.UTC
	if timezone != "" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, fmt.Errorf("load schedule timezone: %w", err)
		}
		loc = loaded
	}
	schedule, err := cronParser.Parse(specText)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse schedule spec: %w", err)
	}
	base := after.In(loc)
	next := schedule.Next(base)
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("cron next fire is zero")
	}
	return next.UTC(), nil
}
