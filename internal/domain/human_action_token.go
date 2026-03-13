package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

func SignHumanActionTokenV1(secret []byte, claims HumanActionClaims) (string, error) {
	if len(secret) == 0 {
		return "", ErrUnauthorized
	}
	if err := ValidateHumanActionClaims(claims); err != nil {
		return "", err
	}
	if claims.ExpiresAt.IsZero() {
		return "", ErrTokenExpired
	}
	if claims.DecisionHash == "" || claims.Nonce == "" {
		return "", ErrInvalidToken
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payloadBytes)

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payloadPart))
	sigPart := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return "v1." + payloadPart + "." + sigPart, nil
}

func VerifyHumanActionTokenV1(secret []byte, tokenString string, now time.Time) (HumanActionClaims, error) {
	var claims HumanActionClaims
	if len(secret) == 0 {
		return claims, ErrUnauthorized
	}
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return claims, ErrInvalidToken
	}
	payloadPart := parts[1]
	sigPart := parts[2]

	gotSig, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return claims, ErrInvalidToken
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payloadPart))
	expectedSig := mac.Sum(nil)
	if !hmac.Equal(gotSig, expectedSig) {
		return claims, ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return claims, ErrInvalidToken
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return claims, ErrInvalidToken
	}
	if claims.ExpiresAt.IsZero() || now.UTC().After(claims.ExpiresAt.UTC()) {
		return claims, ErrTokenExpired
	}
	if claims.DecisionHash == "" || claims.Nonce == "" {
		return claims, ErrInvalidToken
	}
	if err := ValidateHumanActionClaims(claims); err != nil {
		return claims, err
	}
	claims.ActionKind = string(NormalizeHumanActionKind(claims.ActionKind))
	return claims, nil
}
