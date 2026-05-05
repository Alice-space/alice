package automation

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/Alice-space/alice/internal/logging"
)

var (
	goalStartTemplate    = ""
	goalContinueTemplate = ""
	goalTimeoutTemplate  = ""
)

func SetGoalTemplates(start, cont, timeout string) {
	goalStartTemplate = strings.TrimSpace(start)
	goalContinueTemplate = strings.TrimSpace(cont)
	goalTimeoutTemplate = strings.TrimSpace(timeout)
}

type goalPromptData struct {
	Objective string
	Now       string
	Deadline  string
	Elapsed   string
	Remaining string
}

func renderGoalTemplate(tmpl string, data goalPromptData) string {
	if tmpl == "" {
		return data.Objective
	}
	t, err := template.New("goal").Parse(tmpl)
	if err != nil {
		logging.Warnf("goal template parse failed: %v", err)
		return data.Objective
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		logging.Warnf("goal template render failed: %v", err)
		return data.Objective
	}
	return strings.TrimSpace(buf.String())
}
