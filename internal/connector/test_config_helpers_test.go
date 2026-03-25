package connector

import (
	"github.com/Alice-space/alice/internal/config"
)

func strPtr(s string) *string { return &s }

func configForTest() config.Config {
	return config.Config{
		QueueCapacity:     8,
		WorkerConcurrency: 1,
	}
}
