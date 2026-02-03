// Package templates provides embedded HTML templates, CSS, and JS for the web UI.
package templates

import "embed"

// FS contains embedded HTML templates, CSS, JS, SVG, and JSON files.
//
//go:embed *.html *.css *.js *.svg *.json
var FS embed.FS
