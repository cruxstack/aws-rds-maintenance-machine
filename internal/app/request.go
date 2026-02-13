package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/app/templates"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/app/ui"
	internalerrors "github.com/mpz/devops/tools/rds-maint-machine/internal/errors"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/rds"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// Request represents an HTTP request.
type Request struct {
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

// Response is a unified response type.
type Response struct {
	StatusCode  int               `json:"status_code"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        []byte            `json:"body,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
}

// HandleRequest routes incoming requests to the appropriate handler.
func (a *App) HandleRequest(ctx context.Context, req Request) Response {
	start := time.Now()

	resp := a.handleHTTPRequest(ctx, req)

	// Log API requests (skip static assets and UI)
	if !isStaticPath(req.Path) {
		a.Logger.Info("request",
			"method", req.Method,
			"path", req.Path,
			"status", resp.StatusCode,
			"duration_ms", time.Since(start).Milliseconds())
	}

	return resp
}

// isStaticPath returns true for paths that should not be logged (static assets, UI).
func isStaticPath(path string) bool {
	return path == "/" ||
		strings.HasPrefix(path, "/static/") ||
		strings.HasPrefix(path, "/assets/") ||
		strings.HasPrefix(path, "/favicon")
}

// handleHTTPRequest routes HTTP requests.
func (a *App) handleHTTPRequest(ctx context.Context, req Request) Response {
	path := req.Path
	if a.Config.BasePath != "" {
		path = strings.TrimPrefix(path, a.Config.BasePath)
		if path == "" {
			path = "/"
		}
	}

	// Route based on path
	switch {
	case path == "/" && req.Method == "GET":
		return a.handleNewUI()
	case strings.HasPrefix(path, "/assets/") && req.Method == "GET":
		return a.handleNewUIAsset(path)
	case path == "/favicon.svg" && req.Method == "GET":
		return a.handleNewUIFavicon("favicon.svg")
	case path == "/favicon-demo.svg" && req.Method == "GET":
		return a.handleNewUIFavicon("favicon-demo.svg")
	// Legacy static routes (for backwards compatibility during transition)
	case path == "/static/styles.css" && req.Method == "GET":
		return a.handleStaticCSS()
	case path == "/static/main.js" && req.Method == "GET":
		return a.handleStaticJS("main.js")
	case path == "/static/demo.js" && req.Method == "GET":
		return a.handleStaticJS("demo.js")

	case path == "/server/status" && req.Method == "GET":
		return a.handleStatusRequest(req)
	case path == "/server/config" && req.Method == "GET":
		return a.handleConfigRequest(req)
	case path == "/api/operations" && req.Method == "GET":
		return a.handleListOperations(req)
	case path == "/api/operations" && req.Method == "POST":
		return a.handleCreateOperation(ctx, req)
	case path == "/api/operations" && req.Method == "DELETE":
		return a.handleDeleteAllOperations(ctx)
	case strings.HasPrefix(path, "/api/operations/") && strings.HasSuffix(path, "/start") && req.Method == "POST":
		return a.handleStartOperation(ctx, req, extractOperationID(path, "/start"))
	case strings.HasPrefix(path, "/api/operations/") && strings.HasSuffix(path, "/resume") && req.Method == "POST":
		return a.handleResumeOperation(ctx, req, extractOperationID(path, "/resume"))
	case strings.HasPrefix(path, "/api/operations/") && strings.HasSuffix(path, "/pause") && req.Method == "POST":
		return a.handlePauseOperation(ctx, req, extractOperationID(path, "/pause"))
	case strings.HasPrefix(path, "/api/operations/") && strings.HasSuffix(path, "/reset") && req.Method == "POST":
		return a.handleResetOperation(ctx, req, extractOperationID(path, "/reset"))
	case strings.HasPrefix(path, "/api/operations/") && strings.HasSuffix(path, "/events") && req.Method == "GET":
		return a.handleGetEvents(req, extractOperationID(path, "/events"))
	case strings.HasPrefix(path, "/api/operations/") && req.Method == "PATCH":
		return a.handleUpdateOperation(ctx, req, strings.TrimPrefix(path, "/api/operations/"))
	case strings.HasPrefix(path, "/api/operations/") && req.Method == "DELETE":
		return a.handleDeleteOperation(ctx, strings.TrimPrefix(path, "/api/operations/"))
	case strings.HasPrefix(path, "/api/operations/") && req.Method == "GET":
		return a.handleGetOperation(req, strings.TrimPrefix(path, "/api/operations/"))
	case path == "/api/regions" && req.Method == "GET":
		return a.handleListRegions(ctx)
	case strings.HasPrefix(path, "/api/regions/") && strings.HasSuffix(path, "/clusters") && req.Method == "GET":
		region := strings.TrimPrefix(path, "/api/regions/")
		region = strings.TrimSuffix(region, "/clusters")
		return a.handleListClusters(ctx, region)
	case path == "/api/cluster" && req.Method == "GET":
		return a.handleGetClusterInfo(ctx, req)
	case path == "/api/cluster/blue-green" && req.Method == "GET":
		return a.handleGetBlueGreenDeployments(ctx, req)
	case path == "/api/cluster/upgrade-targets" && req.Method == "GET":
		return a.handleGetUpgradeTargets(ctx, req)
	case path == "/api/cluster/instance-types" && req.Method == "GET":
		return a.handleGetInstanceTypes(ctx, req)
	case path == "/api/cluster/proxies" && req.Method == "GET":
		return a.handleGetClusterProxies(ctx, req)
	case path == "/api/cluster/blue-green-prerequisites" && req.Method == "GET":
		return a.handleGetBlueGreenPrerequisites(ctx, req)
	case path == "/api/cluster/events" && req.Method == "GET":
		return a.handleGetClusterEvents(ctx, req)
	case path == "/api/config" && req.Method == "GET":
		return a.handlePublicConfig()
	case strings.HasPrefix(path, "/mock/"):
		return a.handleMockProxy(req)
	default:
		return errorResponse(404, "endpoint not found")
	}
}

// handleStatusRequest returns application status.
func (a *App) handleStatusRequest(req Request) Response {
	if resp := a.checkAdminAuth(req); resp != nil {
		return *resp
	}
	return jsonResponse(200, a.GetStatus())
}

// handleConfigRequest returns redacted configuration.
func (a *App) handleConfigRequest(req Request) Response {
	if resp := a.checkAdminAuth(req); resp != nil {
		return *resp
	}
	return jsonResponse(200, a.Config.Redacted())
}

// handlePublicConfig returns public configuration (no auth required).
// This is used by the React UI to determine if demo mode is enabled.
func (a *App) handlePublicConfig() Response {
	return jsonResponse(200, map[string]any{
		"demo_mode": a.Config.DemoMode,
		"base_path": a.Config.BasePath,
	})
}

// handleListOperations returns all operations.
func (a *App) handleListOperations(req Request) Response {
	return jsonResponse(200, a.ListOperations())
}

// handleGetOperation returns a single operation.
func (a *App) handleGetOperation(req Request, id string) Response {
	op, err := a.GetOperation(id)
	if err != nil {
		return errorResponse(404, err.Error())
	}
	return jsonResponse(200, op)
}

// handleCreateOperation creates a new operation.
func (a *App) handleCreateOperation(ctx context.Context, req Request) Response {
	var createReq CreateOperationRequest
	if err := json.Unmarshal(req.Body, &createReq); err != nil {
		return errorResponse(400, "invalid operation request body")
	}

	op, err := a.CreateOperation(ctx, createReq)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	return jsonResponse(201, op)
}

// handleStartOperation starts an operation.
func (a *App) handleStartOperation(ctx context.Context, req Request, id string) Response {
	if err := a.StartOperation(ctx, id); err != nil {
		return errorResponse(500, err.Error())
	}
	return jsonResponse(200, map[string]string{"status": "started"})
}

// handleResumeOperation resumes a paused operation.
func (a *App) handleResumeOperation(ctx context.Context, req Request, id string) Response {
	var response types.InterventionResponse
	if err := json.Unmarshal(req.Body, &response); err != nil {
		return errorResponse(400, "invalid resume request body")
	}

	if err := a.ResumeOperation(ctx, id, response); err != nil {
		return errorResponse(500, err.Error())
	}
	return jsonResponse(200, map[string]string{"status": "resumed"})
}

// handlePauseOperation pauses a running operation.
func (a *App) handlePauseOperation(ctx context.Context, req Request, id string) Response {
	var body struct {
		Reason string `json:"reason"`
	}
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return errorResponse(400, "invalid pause request body: "+err.Error())
		}
	}

	if err := a.PauseOperation(ctx, id, body.Reason); err != nil {
		return errorResponse(500, err.Error())
	}
	return jsonResponse(200, map[string]string{"status": "paused"})
}

// handleGetEvents returns events for an operation.
func (a *App) handleGetEvents(req Request, id string) Response {
	events, err := a.GetEvents(id)
	if err != nil {
		return errorResponse(404, err.Error())
	}
	return jsonResponse(200, events)
}

// handleResetOperation resets an operation to a specific step in paused state.
func (a *App) handleResetOperation(ctx context.Context, req Request, id string) Response {
	var body struct {
		StepIndex int `json:"step_index"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResponse(400, "invalid reset request body")
	}

	if err := a.Engine.ResetOperationToStep(ctx, id, body.StepIndex); err != nil {
		if internalerrors.IsNotFound(err) {
			return errorResponse(404, err.Error())
		}
		return errorResponse(400, err.Error())
	}

	op, err := a.GetOperation(id)
	if err != nil {
		return errorResponse(404, err.Error())
	}
	return jsonResponse(200, op)
}

// handleUpdateOperation updates an operation (e.g., timeout, pause_before_steps).
func (a *App) handleUpdateOperation(ctx context.Context, req Request, id string) Response {
	var body struct {
		WaitTimeout      int   `json:"wait_timeout"`
		PauseBeforeSteps []int `json:"pause_before_steps"`
	}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return errorResponse(400, "invalid update request body")
	}

	if body.WaitTimeout > 0 {
		if err := a.UpdateOperationTimeout(ctx, id, body.WaitTimeout); err != nil {
			return errorResponse(500, err.Error())
		}
	}

	// Handle pause_before_steps update (can be empty array to clear all)
	if body.PauseBeforeSteps != nil {
		if err := a.Engine.SetPauseBeforeSteps(ctx, id, body.PauseBeforeSteps); err != nil {
			if internalerrors.IsNotFound(err) {
				return errorResponse(404, err.Error())
			}
			return errorResponse(400, err.Error())
		}
	}

	op, err := a.GetOperation(id)
	if err != nil {
		return errorResponse(404, err.Error())
	}
	return jsonResponse(200, op)
}

// handleDeleteOperation deletes an operation that was created but never started.
func (a *App) handleDeleteOperation(ctx context.Context, id string) Response {
	if err := a.DeleteOperation(ctx, id); err != nil {
		// Return 400 for invalid state errors, 404 for not found, 500 for others
		if internalerrors.IsNotFound(err) {
			return errorResponse(404, err.Error())
		}
		if internalerrors.IsCannotDelete(err) {
			return errorResponse(400, err.Error())
		}
		return errorResponse(500, err.Error())
	}
	return jsonResponse(200, map[string]string{"status": "deleted"})
}

// handleDeleteAllOperations deletes all operations (demo mode only).
func (a *App) handleDeleteAllOperations(ctx context.Context) Response {
	if !a.Config.DemoMode {
		return errorResponse(403, "bulk delete only available in demo mode")
	}

	deleted, errors := a.DeleteAllOperations(ctx)
	return jsonResponse(200, map[string]any{
		"deleted": deleted,
		"errors":  errors,
	})
}

// handleListRegions returns available AWS regions.
func (a *App) handleListRegions(ctx context.Context) Response {
	regions, err := a.ListRegions(ctx)
	if err != nil {
		return errorResponse(500, err.Error())
	}
	return jsonResponse(200, map[string]any{
		"regions":        regions,
		"default_region": a.Config.AWSRegion,
	})
}

// handleListClusters returns Aurora clusters in a region.
func (a *App) handleListClusters(ctx context.Context, region string) Response {
	clusters, err := a.ListClusters(ctx, region)
	if err != nil {
		return errorResponse(500, err.Error())
	}
	return jsonResponse(200, clusters)
}

// handleGetClusterInfo returns info about an RDS cluster.
func (a *App) handleGetClusterInfo(ctx context.Context, req Request) Response {
	clusterID := req.Headers["x-cluster-id"]
	region := req.Headers["x-region"]

	if clusterID == "" {
		return errorResponse(400, "missing x-cluster-id header")
	}
	if region == "" {
		region = a.Config.AWSRegion
	}

	info, err := a.GetClusterInfo(ctx, region, clusterID)
	if err != nil {
		return errorResponse(500, err.Error())
	}
	return jsonResponse(200, info)
}

// handleGetBlueGreenDeployments returns Blue-Green deployments for a cluster.
func (a *App) handleGetBlueGreenDeployments(ctx context.Context, req Request) Response {
	clusterID := req.Headers["x-cluster-id"]
	region := req.Headers["x-region"]

	if clusterID == "" {
		return errorResponse(400, "missing x-cluster-id header")
	}
	if region == "" {
		region = a.Config.AWSRegion
	}

	deployments, err := a.GetBlueGreenDeployments(ctx, region, clusterID)
	if err != nil {
		return errorResponse(500, err.Error())
	}
	return jsonResponse(200, deployments)
}

// handleGetInstanceTypes returns available instance types for a cluster's engine.
func (a *App) handleGetInstanceTypes(ctx context.Context, req Request) Response {
	clusterID := req.Headers["x-cluster-id"]
	region := req.Headers["x-region"]

	if clusterID == "" {
		return errorResponse(400, "missing x-cluster-id header")
	}
	if region == "" {
		region = a.Config.AWSRegion
	}

	client, err := a.ClientManager.GetClient(ctx, region)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	// Get cluster info to determine engine and version
	clusterInfo, err := client.GetClusterInfo(ctx, clusterID)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	// Get available instance types
	instanceTypes, err := client.GetOrderableInstanceTypes(ctx, clusterInfo.Engine, clusterInfo.EngineVersion)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	// Get current writer instance type for reference
	var currentInstanceType string
	for _, inst := range clusterInfo.Instances {
		if inst.Role == "writer" {
			currentInstanceType = inst.InstanceType
			break
		}
	}

	response := struct {
		CurrentInstanceType string                      `json:"current_instance_type"`
		Engine              string                      `json:"engine"`
		EngineVersion       string                      `json:"engine_version"`
		InstanceTypes       []rds.OrderableInstanceType `json:"instance_types"`
	}{
		CurrentInstanceType: currentInstanceType,
		Engine:              clusterInfo.Engine,
		EngineVersion:       clusterInfo.EngineVersion,
		InstanceTypes:       instanceTypes,
	}

	return jsonResponse(200, response)
}

// handleGetUpgradeTargets returns valid upgrade targets for a cluster's engine version.
func (a *App) handleGetUpgradeTargets(ctx context.Context, req Request) Response {
	clusterID := req.Headers["x-cluster-id"]
	region := req.Headers["x-region"]

	if clusterID == "" {
		return errorResponse(400, "missing x-cluster-id header")
	}
	if region == "" {
		region = a.Config.AWSRegion
	}

	client, err := a.ClientManager.GetClient(ctx, region)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	// Get cluster info to determine engine and version
	clusterInfo, err := client.GetClusterInfo(ctx, clusterID)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	// Get valid upgrade targets
	targets, err := client.GetValidUpgradeTargets(ctx, clusterInfo.Engine, clusterInfo.EngineVersion)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	// For Aurora PostgreSQL, Blue-Green deployments are generally supported
	// Include all targets (the SupportsBlueGreen flag is informational)
	bgTargets := targets

	response := struct {
		CurrentVersion string              `json:"current_version"`
		Engine         string              `json:"engine"`
		UpgradeTargets []rds.UpgradeTarget `json:"upgrade_targets"`
	}{
		CurrentVersion: clusterInfo.EngineVersion,
		Engine:         clusterInfo.Engine,
		UpgradeTargets: bgTargets,
	}

	return jsonResponse(200, response)
}

// handleGetClusterEvents returns recent RDS events for a cluster (for debugging).
func (a *App) handleGetClusterEvents(ctx context.Context, req Request) Response {
	clusterID := req.Headers["x-cluster-id"]
	region := req.Headers["x-region"]

	if clusterID == "" {
		return errorResponse(400, "missing x-cluster-id header")
	}
	if region == "" {
		region = a.Config.AWSRegion
	}

	client, err := a.ClientManager.GetClient(ctx, region)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	events, err := client.GetRecentClusterEvents(ctx, clusterID, 50)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	return jsonResponse(200, map[string]any{
		"cluster_id": clusterID,
		"events":     events,
	})
}

// handleGetBlueGreenPrerequisites checks if a cluster meets Blue-Green deployment prerequisites.
func (a *App) handleGetBlueGreenPrerequisites(ctx context.Context, req Request) Response {
	clusterID := req.Headers["x-cluster-id"]
	region := req.Headers["x-region"]

	if clusterID == "" {
		return errorResponse(400, "missing x-cluster-id header")
	}
	if region == "" {
		region = a.Config.AWSRegion
	}

	client, err := a.ClientManager.GetClient(ctx, region)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	prereqs, err := client.CheckBlueGreenPrerequisites(ctx, clusterID)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	return jsonResponse(200, prereqs)
}

// handleGetClusterProxies returns RDS Proxies targeting a cluster.
func (a *App) handleGetClusterProxies(ctx context.Context, req Request) Response {
	clusterID := req.Headers["x-cluster-id"]
	region := req.Headers["x-region"]

	if clusterID == "" {
		return errorResponse(400, "missing x-cluster-id header")
	}
	if region == "" {
		region = a.Config.AWSRegion
	}

	client, err := a.ClientManager.GetClient(ctx, region)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	// Find proxies targeting this cluster
	proxies, err := client.FindProxiesForCluster(ctx, clusterID)
	if err != nil {
		return errorResponse(500, err.Error())
	}

	response := struct {
		ClusterID string                 `json:"cluster_id"`
		Proxies   []rds.ProxyWithTargets `json:"proxies"`
	}{
		ClusterID: clusterID,
		Proxies:   proxies,
	}

	return jsonResponse(200, response)
}

// handleUI returns the HTML UI (legacy template-based UI).
func (a *App) handleUI(req Request) Response {
	// Use demo UI if in demo mode
	var html string
	var err error
	if a.Config.DemoMode {
		html, err = getDemoDashboardHTML()
	} else {
		html, err = getDashboardHTML()
	}
	if err != nil {
		return errorResponse(500, "failed to load UI template: "+err.Error())
	}
	return Response{
		StatusCode:  200,
		ContentType: "text/html",
		Body:        []byte(html),
	}
}

// handleNewUI serves the new React SPA index.html.
func (a *App) handleNewUI() Response {
	data, err := ui.DistFS.ReadFile("dist/index.html")
	if err != nil {
		return errorResponse(500, "failed to load UI: "+err.Error())
	}
	return Response{
		StatusCode:  200,
		ContentType: "text/html",
		Body:        data,
	}
}

// handleNewUIAsset serves static assets (JS, CSS) from the new React build.
func (a *App) handleNewUIAsset(path string) Response {
	// path is like "/assets/index-abc123.js"
	// We need to read from "dist/assets/..."
	filePath := "dist" + path
	data, err := ui.DistFS.ReadFile(filePath)
	if err != nil {
		return errorResponse(404, "asset not found")
	}

	contentType := "application/octet-stream"
	ext := filepath.Ext(path)
	switch ext {
	case ".js":
		contentType = "application/javascript"
	case ".css":
		contentType = "text/css"
	case ".svg":
		contentType = "image/svg+xml"
	case ".png":
		contentType = "image/png"
	case ".ico":
		contentType = "image/x-icon"
	case ".woff2":
		contentType = "font/woff2"
	case ".woff":
		contentType = "font/woff"
	}

	return Response{
		StatusCode:  200,
		ContentType: contentType,
		Headers:     map[string]string{"Cache-Control": "public, max-age=31536000, immutable"},
		Body:        data,
	}
}

// handleNewUIFavicon serves favicon from the new React build.
func (a *App) handleNewUIFavicon(filename string) Response {
	data, err := ui.DistFS.ReadFile("dist/" + filename)
	if err != nil {
		return errorResponse(404, "favicon not found")
	}
	return Response{
		StatusCode:  200,
		ContentType: "image/svg+xml",
		Headers:     map[string]string{"Cache-Control": "public, max-age=86400"},
		Body:        data,
	}
}

// handleStaticCSS serves the shared CSS file.
func (a *App) handleStaticCSS() Response {
	data, err := templates.FS.ReadFile("styles.css")
	if err != nil {
		return errorResponse(500, "failed to load CSS: "+err.Error())
	}
	return Response{
		StatusCode:  200,
		ContentType: "text/css",
		Headers:     map[string]string{"Cache-Control": "public, max-age=3600"},
		Body:        data,
	}
}

// handleStaticJS serves JavaScript files.
func (a *App) handleStaticJS(filename string) Response {
	data, err := templates.FS.ReadFile(filename)
	if err != nil {
		return errorResponse(500, "failed to load JS: "+err.Error())
	}
	return Response{
		StatusCode:  200,
		ContentType: "application/javascript",
		Headers:     map[string]string{"Cache-Control": "public, max-age=3600"},
		Body:        data,
	}
}

// handleFavicon serves the favicon SVG.
func (a *App) handleFavicon(filename string) Response {
	data, err := templates.FS.ReadFile(filename)
	if err != nil {
		return errorResponse(500, "failed to load favicon: "+err.Error())
	}
	return Response{
		StatusCode:  200,
		ContentType: "image/svg+xml",
		Headers:     map[string]string{"Cache-Control": "public, max-age=86400"},
		Body:        data,
	}
}

// handleMockProxy proxies requests to the mock server (demo mode only).
func (a *App) handleMockProxy(req Request) Response {
	if a.Config.MockEndpoint == "" {
		return errorResponse(404, "mock server not configured")
	}

	// Build the target URL
	targetURL := a.Config.MockEndpoint + req.Path

	// Create HTTP request
	httpReq, err := http.NewRequest(req.Method, targetURL, bytes.NewReader(req.Body))
	if err != nil {
		return errorResponse(500, "failed to create proxy request: "+err.Error())
	}

	// Copy headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Make the request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return errorResponse(502, "mock server unavailable: "+err.Error())
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorResponse(502, "failed to read mock response: "+err.Error())
	}

	return Response{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Headers:     map[string]string{"Content-Type": resp.Header.Get("Content-Type")},
		Body:        body,
	}
}

func extractOperationID(path, suffix string) string {
	path = strings.TrimPrefix(path, "/api/operations/")
	path = strings.TrimSuffix(path, suffix)
	return path
}

func jsonResponse(status int, data any) Response {
	body, err := json.Marshal(data)
	if err != nil {
		return errorResponse(500, "failed to encode response")
	}
	return Response{
		StatusCode:  status,
		ContentType: "application/json",
		Headers:     map[string]string{"Content-Type": "application/json"},
		Body:        body,
	}
}

func errorResponse(status int, message string) Response {
	body, _ := json.Marshal(map[string]string{"error": message})
	return Response{
		StatusCode:  status,
		ContentType: "application/json",
		Headers:     map[string]string{"Content-Type": "application/json"},
		Body:        body,
	}
}

func (a *App) checkAdminAuth(req Request) *Response {
	if a.Config.AdminToken == "" {
		return nil
	}

	authHeader := req.Headers["authorization"]
	if authHeader == "" {
		resp := errorResponse(401, "missing authorization header")
		return &resp
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		token = strings.TrimPrefix(authHeader, "bearer ")
	}

	if token != a.Config.AdminToken {
		resp := errorResponse(401, "invalid authorization token")
		return &resp
	}

	return nil
}
