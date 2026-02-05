package app

import (
	"context"
	"testing"
	"time"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/config"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/machine"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/notifiers"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/storage"
)

// testApp creates a minimal App for testing HTTP routing.
func testApp(t *testing.T) *App {
	t.Helper()

	cfg := &config.Config{
		AWSRegion:  "us-east-1",
		DemoMode:   true,
		AdminToken: "test-admin-token",
	}

	engine := machine.NewEngine(machine.EngineConfig{
		Store:               &storage.NullStore{},
		DefaultRegion:       "us-east-1",
		DefaultWaitTimeout:  5 * time.Minute,
		DefaultPollInterval: 1 * time.Second,
	})

	return NewWithEngine(cfg, engine, &notifiers.NullNotifier{})
}

func TestHandleRequest_HTTPRouting(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		method         string
		path           string
		body           []byte
		headers        map[string]string
		wantStatus     int
		wantBodySubstr string
	}{
		{
			name:       "GET / returns HTML",
			method:     "GET",
			path:       "/",
			wantStatus: 200,
		},
		{
			name:       "GET /api/operations returns list",
			method:     "GET",
			path:       "/api/operations",
			wantStatus: 200,
		},
		{
			name:       "GET unknown path returns 404",
			method:     "GET",
			path:       "/api/unknown",
			wantStatus: 404,
		},
		{
			name:       "POST /api/operations without body returns error",
			method:     "POST",
			path:       "/api/operations",
			body:       []byte("not json"),
			wantStatus: 400,
		},
		{
			name:       "GET /server/status without auth returns 401",
			method:     "GET",
			path:       "/server/status",
			wantStatus: 401,
		},
		{
			name:       "GET /server/status with auth returns 200",
			method:     "GET",
			path:       "/server/status",
			headers:    map[string]string{"authorization": "Bearer test-admin-token"},
			wantStatus: 200,
		},
		{
			name:       "GET /server/config with auth returns 200",
			method:     "GET",
			path:       "/server/config",
			headers:    map[string]string{"authorization": "Bearer test-admin-token"},
			wantStatus: 200,
		},
		{
			name:           "GET nonexistent operation returns 404",
			method:         "GET",
			path:           "/api/operations/nonexistent-id",
			wantStatus:     404,
			wantBodySubstr: "not found",
		},
		{
			name:       "DELETE nonexistent operation returns 404",
			method:     "DELETE",
			path:       "/api/operations/nonexistent-id",
			wantStatus: 404,
		},
		{
			name:       "GET /static/styles.css returns CSS",
			method:     "GET",
			path:       "/static/styles.css",
			wantStatus: 200,
		},
		{
			name:       "GET /static/main.js returns JS",
			method:     "GET",
			path:       "/static/main.js",
			wantStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{
				Method:  tt.method,
				Path:    tt.path,
				Body:    tt.body,
				Headers: tt.headers,
			}

			resp := app.HandleRequest(ctx, req)

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("got status %d, want %d. Body: %s", resp.StatusCode, tt.wantStatus, string(resp.Body))
			}

			if tt.wantBodySubstr != "" && len(resp.Body) > 0 {
				body := string(resp.Body)
				if !contains(body, tt.wantBodySubstr) {
					t.Errorf("body %q does not contain %q", body, tt.wantBodySubstr)
				}
			}
		})
	}
}

func TestHandleRequest_BasePath(t *testing.T) {
	cfg := &config.Config{
		AWSRegion: "us-east-1",
		DemoMode:  true,
		BasePath:  "/rds-maint",
	}

	engine := machine.NewEngine(machine.EngineConfig{
		Store:         &storage.NullStore{},
		DefaultRegion: "us-east-1",
	})

	app := NewWithEngine(cfg, engine, &notifiers.NullNotifier{})
	ctx := context.Background()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "base path stripped - root",
			path:       "/rds-maint/",
			wantStatus: 200,
		},
		{
			name:       "base path stripped - api",
			path:       "/rds-maint/api/operations",
			wantStatus: 200,
		},
		{
			name:       "base path stripped - static",
			path:       "/rds-maint/static/styles.css",
			wantStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{
				Method: "GET",
				Path:   tt.path,
			}

			resp := app.HandleRequest(ctx, req)

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("got status %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestIsStaticPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/static/main.js", true},
		{"/static/styles.css", true},
		{"/favicon.svg", true},
		{"/favicon-demo.svg", true},
		{"/api/operations", false},
		{"/api/regions", false},
		{"/server/status", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isStaticPath(tt.path)
			if got != tt.want {
				t.Errorf("isStaticPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
