package domain

import "errors"

// Common errors used across the application.
var (
	// Auth errors
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidToken       = errors.New("invalid token")
	ErrTokenExpired       = errors.New("token expired")
	ErrMissingTokenClaims = errors.New("missing token integrity claims")

	// Webhook errors
	ErrUnsupportedWebhook         = errors.New("unsupported webhook transport")
	ErrWebhookSecretNotConfigured = errors.New("webhook secret is not configured")
	ErrMissingGitHubEvent         = errors.New("missing github event header")
	ErrMissingGitHubDelivery      = errors.New("missing github delivery id")
	ErrMissingGitHubSignature     = errors.New("missing github signature")
	ErrInvalidGitHubSignature     = errors.New("invalid github signature")
	ErrMissingGitLabEvent         = errors.New("missing gitlab event header")
	ErrInvalidGitLabToken         = errors.New("invalid gitlab webhook token")
)
