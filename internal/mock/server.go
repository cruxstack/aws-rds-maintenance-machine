package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mpz/devops/tools/rds-maint-machine/internal/mock/templates"
)

// Server is the mock RDS HTTP server.
type Server struct {
	state   *State
	logger  *slog.Logger
	mux     *http.ServeMux
	verbose bool
}

// NewServer creates a new mock RDS server.
func NewServer(state *State, logger *slog.Logger, verbose bool) *Server {
	s := &Server{
		state:   state,
		logger:  logger,
		mux:     http.NewServeMux(),
		verbose: verbose,
	}
	s.registerRoutes()
	return s
}

// registerRoutes sets up the HTTP routes.
func (s *Server) registerRoutes() {
	// RDS API endpoint (AWS uses POST to / with Action parameter)
	s.mux.HandleFunc("/", s.handleRDSAction)

	// Mock management API (for demo UI)
	s.mux.HandleFunc("/mock/state", s.handleMockState)
	s.mux.HandleFunc("/mock/reset", s.handleMockReset)
	s.mux.HandleFunc("/mock/timing", s.handleMockTiming)
	s.mux.HandleFunc("/mock/faults", s.handleMockFaults)
	s.mux.HandleFunc("/mock/faults/", s.handleMockFaultByID)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for demo UI (cross-origin requests from main server)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight requests
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.mux.ServeHTTP(w, r)
}

// handleRDSAction routes RDS API calls based on the Action parameter.
func (s *Server) handleRDSAction(w http.ResponseWriter, r *http.Request) {
	// Skip mock management routes
	if strings.HasPrefix(r.URL.Path, "/mock/") {
		http.NotFound(w, r)
		return
	}

	// Only handle POST requests for RDS API
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the form data to get Action
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendErrorResponse(w, "InternalError", "failed to read request body", 500)
		return
	}
	defer r.Body.Close()

	values, err := url.ParseQuery(string(body))
	if err != nil {
		s.sendErrorResponse(w, "InvalidParameterValue", "failed to parse request body", 400)
		return
	}

	action := values.Get("Action")
	if action == "" {
		s.sendErrorResponse(w, "MissingAction", "missing Action parameter", 400)
		return
	}

	if s.verbose {
		s.logger.Debug("handling RDS API call", slog.String("action", action))
	}

	// Check for fault injection
	faultResult := s.state.Faults().Check(action, "")
	if faultResult.ShouldFail {
		s.sendErrorResponse(w, faultResult.ErrorCode, faultResult.ErrorMsg, 400)
		return
	}
	if faultResult.ExtraDelay > 0 {
		time.Sleep(time.Duration(faultResult.ExtraDelay) * time.Millisecond)
	}

	// Route to appropriate handler
	switch action {
	case "DescribeRegions":
		s.handleDescribeRegions(w, values)
	case "DescribeDBClusters":
		s.handleDescribeDBClusters(w, values)
	case "DescribeDBInstances":
		s.handleDescribeDBInstances(w, values)
	case "ListTagsForResource":
		s.handleListTagsForResource(w, values)
	case "CreateDBInstance":
		s.handleCreateDBInstance(w, values)
	case "ModifyDBInstance":
		s.handleModifyDBInstance(w, values)
	case "DeleteDBInstance":
		s.handleDeleteDBInstance(w, values)
	case "FailoverDBCluster":
		s.handleFailoverDBCluster(w, values)
	case "ModifyDBCluster":
		s.handleModifyDBCluster(w, values)
	case "CreateDBClusterSnapshot":
		s.handleCreateDBClusterSnapshot(w, values)
	case "DescribeDBClusterSnapshots":
		s.handleDescribeDBClusterSnapshots(w, values)
	// Cluster Parameter Group actions
	case "DescribeDBClusterParameterGroups":
		s.handleDescribeDBClusterParameterGroups(w, values)
	case "DescribeDBClusterParameters":
		s.handleDescribeDBClusterParameters(w, values)
	case "CreateDBClusterParameterGroup":
		s.handleCreateDBClusterParameterGroup(w, values)
	case "ModifyDBClusterParameterGroup":
		s.handleModifyDBClusterParameterGroup(w, values)
	// Instance Parameter Group actions
	case "DescribeDBParameterGroups":
		s.handleDescribeDBParameterGroups(w, values)
	case "DescribeDBParameters":
		s.handleDescribeDBParameters(w, values)
	case "CreateDBParameterGroup":
		s.handleCreateDBParameterGroup(w, values)
	case "ModifyDBParameterGroup":
		s.handleModifyDBParameterGroup(w, values)
	// Blue-Green Deployment actions
	case "CreateBlueGreenDeployment":
		s.handleCreateBlueGreenDeployment(w, values)
	case "DescribeBlueGreenDeployments":
		s.handleDescribeBlueGreenDeployments(w, values)
	case "SwitchoverBlueGreenDeployment":
		s.handleSwitchoverBlueGreenDeployment(w, values)
	case "DeleteBlueGreenDeployment":
		s.handleDeleteBlueGreenDeployment(w, values)
	// Cluster deletion (for cleanup)
	case "DeleteDBCluster":
		s.handleDeleteDBCluster(w, values)
	// Engine version info
	case "DescribeDBEngineVersions":
		s.handleDescribeDBEngineVersions(w, values)
	// Orderable instance types
	case "DescribeOrderableDBInstanceOptions":
		s.handleDescribeOrderableDBInstanceOptions(w, values)
	// Instance reboot
	case "RebootDBInstance":
		s.handleRebootDBInstance(w, values)
	// RDS Proxy actions
	case "DescribeDBProxies":
		s.handleDescribeDBProxies(w, values)
	case "DescribeDBProxyTargetGroups":
		s.handleDescribeDBProxyTargetGroups(w, values)
	case "DescribeDBProxyTargets":
		s.handleDescribeDBProxyTargets(w, values)
	case "RegisterDBProxyTargets":
		s.handleRegisterDBProxyTargets(w, values)
	case "DeregisterDBProxyTargets":
		s.handleDeregisterDBProxyTargets(w, values)
	default:
		s.sendErrorResponse(w, "InvalidAction", fmt.Sprintf("unsupported action: %s", action), 400)
	}
}

// sendErrorResponse sends an AWS-style XML error response.
func (s *Server) sendErrorResponse(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	data := struct {
		Code    string
		Message string
	}{Code: code, Message: message}
	if err := templates.Execute(w, "error.xml", data); err != nil {
		s.logger.Error("failed to execute error template", "error", err)
	}
}

// Mock management API handlers

func (s *Server) handleMockState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := struct {
		Clusters             []*MockCluster             `json:"clusters"`
		Instances            []*MockInstance            `json:"instances"`
		Snapshots            []*MockSnapshot            `json:"snapshots"`
		BlueGreenDeployments []*MockBlueGreenDeployment `json:"blue_green_deployments"`
		Proxies              []*MockDBProxy             `json:"proxies"`
		Timing               TimingConfig               `json:"timing"`
		Faults               []Fault                    `json:"faults"`
	}{
		Clusters:             s.state.ListClusters(),
		Instances:            s.state.ListInstances(),
		Snapshots:            s.state.ListSnapshots(),
		BlueGreenDeployments: s.state.ListBlueGreenDeployments(),
		Proxies:              s.state.ListProxies(),
		Timing:               s.state.GetTiming(),
		Faults:               s.state.Faults().ListFaults(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (s *Server) handleMockReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.state.Reset()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
}

func (s *Server) handleMockTiming(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.state.GetTiming())

	case http.MethodPost:
		var timing TimingConfig
		if err := json.NewDecoder(r.Body).Decode(&timing); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		s.state.SetTiming(timing)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(timing)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMockFaults(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.state.Faults().ListFaults())

	case http.MethodPost:
		var fault Fault
		if err := json.NewDecoder(r.Body).Decode(&fault); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		id := s.state.Faults().AddFault(fault)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": id})

	case http.MethodDelete:
		s.state.Faults().ClearAll()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMockFaultByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/mock/faults/")
	if id == "" {
		http.Error(w, "missing fault ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if s.state.Faults().RemoveFault(id) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
		} else {
			http.Error(w, "fault not found", http.StatusNotFound)
		}

	case http.MethodPut:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if s.state.Faults().EnableFault(id, body.Enabled) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
		} else {
			http.Error(w, "fault not found", http.StatusNotFound)
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
