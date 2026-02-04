// Package templates provides embedded HTML templates, CSS, and JS for the web UI.
package templates

import "embed"

// FS contains embedded HTML templates, CSS, JS, and SVG files.
//
//go:embed *.html *.css *.js *.svg
var FS embed.FS
