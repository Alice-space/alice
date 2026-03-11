package prompts

import (
	"bytes"
	"embed"
	"fmt"

	"github.com/Masterminds/sprig/v3"
	"text/template"
)

// contentFS contains all embedded template files.
//
//go:embed templates/*.tmpl
var contentFS embed.FS

var (
	templates       map[string]string
	parsedTemplates *template.Template
)

func init() {
	templates = make(map[string]string)
	loadTemplates()
}

// Template names
const (
	ReceptionAssessmentSystem = "reception_assessment_system"
	DirectAnswerSystem        = "direct_answer_system"
	DirectAnswerBase          = "direct_answer_base"
	DirectAnswerDefault       = "direct_answer_default"
	LocalAgentOutputFormat    = "local_agent_output_format"
	ReceptionAssessmentTask   = "reception_assessment_task"
	DirectAnswerTask          = "direct_answer_task"
	DirectAnswerExecutorTask  = "direct_answer_executor_task"
)

// loadTemplates loads all templates from embedded files
func loadTemplates() {
	files := []string{
		ReceptionAssessmentSystem,
		DirectAnswerSystem,
		DirectAnswerBase,
		DirectAnswerDefault,
		LocalAgentOutputFormat,
		ReceptionAssessmentTask,
		DirectAnswerTask,
		DirectAnswerExecutorTask,
	}

	for _, name := range files {
		content, err := contentFS.ReadFile("templates/" + name + ".tmpl")
		if err != nil {
			continue
		}
		templates[name] = string(content)
	}

	// Parse all templates with sprig functions for enhanced template capabilities
	parsedTemplates = template.New("prompts").Funcs(sprig.TxtFuncMap())
	for name, content := range templates {
		_, err := parsedTemplates.New(name).Parse(content)
		if err != nil {
			fmt.Printf("Warning: failed to parse template %s: %v\n", name, err)
		}
	}
}

// Get returns a static template by name
func Get(name string) string {
	if content, ok := templates[name]; ok {
		return content
	}
	return ""
}

// MustGet returns a static template by name, panics if not found
func MustGet(name string) string {
	content := Get(name)
	if content == "" {
		panic(fmt.Sprintf("prompt template %q not found", name))
	}
	return content
}

// Render renders a template with the given data using sprig functions
func Render(name string, data interface{}) (string, error) {
	tmpl := parsedTemplates.Lookup(name)
	if tmpl == nil {
		return "", fmt.Errorf("template %q not found", name)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %q: %w", name, err)
	}

	return buf.String(), nil
}

// MustRender renders a template with the given data, panics on error
func MustRender(name string, data interface{}) string {
	result, err := Render(name, data)
	if err != nil {
		panic(err)
	}
	return result
}

// Reload reloads all templates (useful for hot-reload scenarios)
func Reload() {
	templates = make(map[string]string)
	parsedTemplates = template.New("prompts").Funcs(sprig.TxtFuncMap())
	loadTemplates()
}
