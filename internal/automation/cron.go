package automation

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

func validateCronExpression(raw string) error {
	expr := strings.TrimSpace(raw)
	if expr == "" {
		return fmt.Errorf("invalid cron_expr %q: empty expression", raw)
	}
	if _, err := cronParser.Parse(expr); err != nil {
		return fmt.Errorf("invalid cron_expr %q: %w", raw, err)
	}
	return nil
}

func nextCronRunAt(from time.Time, raw string) (time.Time, error) {
	expr := strings.TrimSpace(raw)
	if expr == "" {
		return time.Time{}, fmt.Errorf("invalid cron_expr %q: empty expression", raw)
	}
	schedule, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron_expr %q: %w", raw, err)
	}
	if from.IsZero() {
		from = time.Now().UTC()
	}
	return schedule.Next(from.UTC()), nil
}
