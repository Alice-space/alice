package prompting

import (
	"strings"
)

func ComposePromptPrefix(loader *Loader, promptPrefix string, personality string, noReplyToken string) (string, error) {
	return strings.TrimSpace(promptPrefix), nil
}
