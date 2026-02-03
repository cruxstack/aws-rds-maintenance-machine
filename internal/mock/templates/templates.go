// Package templates provides embedded XML templates for mock RDS API responses.
package templates

import (
	"embed"
	"html/template"
	"io"
	"sync"
)

//go:embed *.xml
var FS embed.FS

var (
	tmpl     *template.Template
	tmplOnce sync.Once
	tmplErr  error
)

// Templates returns the parsed templates.
func Templates() (*template.Template, error) {
	tmplOnce.Do(func() {
		tmpl, tmplErr = template.ParseFS(FS, "*.xml")
	})
	return tmpl, tmplErr
}

// Execute renders a template by name to the writer.
func Execute(w io.Writer, name string, data any) error {
	t, err := Templates()
	if err != nil {
		return err
	}
	return t.ExecuteTemplate(w, name, data)
}
