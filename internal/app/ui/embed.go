// Package ui provides embedded static files for the React web UI.
package ui

import "embed"

// DistFS contains the built Vite/React application from ui/dist/.
// This must be populated by copying ui/dist/* to internal/app/ui/dist/ before building.
//
//go:embed dist/*
var DistFS embed.FS
