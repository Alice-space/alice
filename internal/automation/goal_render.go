package automation

import (
	"bytes"
	"strings"
	"sync/atomic"
	"text/template"

	"github.com/Alice-space/alice/internal/logging"
)

type goalTemplates struct {
	continueTemplate string
	timeoutTemplate  string
}

var goalTemplatesVal atomic.Value

func init() {
	goalTemplatesVal.Store(goalTemplates{})
}

func SetGoalTemplates(cont, timeout string) {
	goalTemplatesVal.Store(goalTemplates{
		continueTemplate: strings.TrimSpace(cont),
		timeoutTemplate:  strings.TrimSpace(timeout),
	})
}

func getGoalTemplates() goalTemplates {
	v, _ := goalTemplatesVal.Load().(goalTemplates)
	return v
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
