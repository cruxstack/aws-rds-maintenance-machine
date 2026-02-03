// Package httputil provides HTTP utilities shared between server modes.
package httputil

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/app"
)

// RequestHandler handles HTTP requests by converting them to app.Request
// and returning app.Response.
type RequestHandler struct {
	app    *app.App
	logger *slog.Logger
}

// NewRequestHandler creates a new HTTP request handler.
func NewRequestHandler(a *app.App, logger *slog.Logger) *RequestHandler {
	return &RequestHandler{
		app:    a,
		logger: logger,
	}
}

// ServeHTTP implements http.Handler interface.
func (h *RequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, "unable to read request body", http.StatusBadRequest)
		return
	}

	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[strings.ToLower(key)] = values[0]
		}
	}

	req := app.Request{
		Type:    app.RequestTypeHTTP,
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: headers,
		Body:    body,
	}

	resp := h.app.HandleRequest(r.Context(), req)

	for key, value := range resp.Headers {
		w.Header().Set(key, value)
	}
	if resp.ContentType != "" && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}

	w.WriteHeader(resp.StatusCode)
	if len(resp.Body) > 0 {
		if _, err := w.Write(resp.Body); err != nil && h.logger != nil {
			h.logger.Debug("failed to write response body", slog.String("error", err.Error()))
		}
	}
}
