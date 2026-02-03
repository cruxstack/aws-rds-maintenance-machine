package app

import (
	"bytes"
	"html/template"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/app/templates"
)

// DashboardData contains data passed to the dashboard template.
type DashboardData struct {
	DemoMode bool
}

// renderDashboard renders the dashboard template with the given data.
func renderDashboard(data DashboardData) (string, error) {
	tmplContent, err := templates.FS.ReadFile("dashboard.html")
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("dashboard").Parse(string(tmplContent))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// getDashboardHTML returns the HTML for the production dashboard.
func getDashboardHTML() (string, error) {
	return renderDashboard(DashboardData{DemoMode: false})
}

// getDemoDashboardHTML returns the HTML for the demo dashboard.
func getDemoDashboardHTML() (string, error) {
	return renderDashboard(DashboardData{DemoMode: true})
}
