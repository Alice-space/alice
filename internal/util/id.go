package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func NewID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s_%d", sanitize(prefix), time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%s", sanitize(prefix), hex.EncodeToString(b))
}

func sanitize(prefix string) string {
	p := strings.TrimSpace(prefix)
	if p == "" {
		return "id"
	}
	p = strings.ReplaceAll(p, " ", "_")
	return strings.ToLower(p)
}
