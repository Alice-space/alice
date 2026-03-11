package ingress

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"alice/internal/domain"
	"github.com/gin-gonic/gin"
)

func (h *HTTPIngress) handleWebhook(sourceKind, transportKind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}

		if err := h.verifyWebhook(c, transportKind, body); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		var in NormalizedEvent
		if err := json.Unmarshal(body, &in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		in.RequestID = ""
		in.TaskID = ""
		in.SourceKind = sourceKind
		in.TransportKind = transportKind
		in.Verified = true

		if deliveryID := h.webhookDeliveryID(c, transportKind); deliveryID != "" {
			in.IdempotencyKey = transportKind + ":" + deliveryID
		}

		evt := toExternalEvent(in)
		result, err := h.runtime.IngestExternalEvent(c.Request.Context(), evt, h.reception)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, writeAcceptedFromResult(result, ""))
	}
}

func (h *HTTPIngress) verifyWebhook(c *gin.Context, transportKind string, body []byte) error {
	switch transportKind {
	case "github":
		return h.verifyGitHubWebhook(c, body)
	case "gitlab":
		return h.verifyGitLabWebhook(c)
	default:
		return domain.ErrUnsupportedWebhook
	}
}

func (h *HTTPIngress) verifyGitHubWebhook(c *gin.Context, body []byte) error {
	if len(h.gitHubSecret) == 0 {
		return domain.ErrWebhookSecretNotConfigured
	}
	if c.GetHeader("X-GitHub-Event") == "" {
		return domain.ErrMissingGitHubEvent
	}
	if c.GetHeader("X-GitHub-Delivery") == "" {
		return domain.ErrMissingGitHubDelivery
	}

	header := strings.TrimSpace(c.GetHeader("X-Hub-Signature-256"))
	if !strings.HasPrefix(header, "sha256=") {
		return domain.ErrMissingGitHubSignature
	}

	signatureHex := strings.TrimPrefix(header, "sha256=")
	got, err := hex.DecodeString(signatureHex)
	if err != nil {
		return domain.ErrInvalidGitHubSignature
	}

	mac := hmac.New(sha256.New, h.gitHubSecret)
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	if !hmac.Equal(got, expected) {
		return domain.ErrInvalidGitHubSignature
	}
	return nil
}

func (h *HTTPIngress) verifyGitLabWebhook(c *gin.Context) error {
	if strings.TrimSpace(h.gitLabSecret) == "" {
		return domain.ErrWebhookSecretNotConfigured
	}
	if c.GetHeader("X-Gitlab-Event") == "" {
		return domain.ErrMissingGitLabEvent
	}
	if c.GetHeader("X-Gitlab-Token") != h.gitLabSecret {
		return domain.ErrInvalidGitLabToken
	}
	return nil
}

func (h *HTTPIngress) webhookDeliveryID(c *gin.Context, transportKind string) string {
	switch transportKind {
	case "github":
		return strings.TrimSpace(c.GetHeader("X-GitHub-Delivery"))
	case "gitlab":
		if v := strings.TrimSpace(c.GetHeader("X-Gitlab-Event-UUID")); v != "" {
			return v
		}
		return strings.TrimSpace(c.GetHeader("X-Request-Id"))
	default:
		return ""
	}
}
