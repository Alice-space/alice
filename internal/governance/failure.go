package governance

import (
	"errors"
	"net"
	"strings"

	"github.com/Alice-space/alice/internal/domain"
)

func ClassifyFailure(err error) domain.FailureInfo {
	if err == nil {
		return domain.FailureInfo{}
	}
	msg := err.Error()
	info := domain.FailureInfo{
		Summary:   msg,
		LastError: msg,
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		info.Source = domain.FailureSourceIntegration
		info.Semantic = domain.FailureSemanticRetryable
		info.Recoverable = true
		return info
	}
	switch {
	case strings.Contains(msg, "timeout"):
		info.Source = domain.FailureSourceExecutor
		info.Semantic = domain.FailureSemanticRetryable
		info.Recoverable = true
	case strings.Contains(msg, "permission"):
		info.Source = domain.FailureSourcePolicyBlocked
		info.Semantic = domain.FailureSemanticHumanRequired
		info.Recoverable = false
	case strings.Contains(msg, "budget"):
		info.Source = domain.FailureSourceResourceExhausted
		info.Semantic = domain.FailureSemanticHumanRequired
		info.Recoverable = false
	case strings.Contains(msg, "not comparable"):
		info.Source = domain.FailureSourceDataInconsistent
		info.Semantic = domain.FailureSemanticHumanRequired
		info.Recoverable = false
	default:
		info.Source = domain.FailureSourceTerminalLogic
		info.Semantic = domain.FailureSemanticTerminal
		info.Recoverable = false
	}
	return info
}
