package connector

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v4"
)

func (s *LarkSender) withFeishuRetry(ctx context.Context, run func() error) error {
	if run == nil {
		return errors.New("feishu operation is nil")
	}
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 200 * time.Millisecond
	bo.MaxInterval = 1 * time.Second
	bo.MaxElapsedTime = 3 * time.Second
	bo.Multiplier = 2
	bo.RandomizationFactor = 0.1
	boCtx := backoff.WithContext(bo, ctx)
	return backoff.Retry(func() error {
		err := run()
		if err == nil {
			return nil
		}
		if !isRetryableFeishuError(err) {
			return backoff.Permanent(err)
		}
		return err
	}, boCtx)
}

func isRetryableFeishuError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *feishuAPIError
	return !errors.As(err, &apiErr)
}
