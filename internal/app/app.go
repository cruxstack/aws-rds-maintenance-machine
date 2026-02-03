// Package app provides the core application logic for the RDS maintenance machine.
package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/cockroachdb/errors"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/config"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/machine"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/notifiers"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/rds"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/storage"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/types"
)

// App is the main application instance.
type App struct {
	Config        *config.Config
	Logger        *slog.Logger
	Engine        *machine.Engine
	ClientManager *rds.ClientManager
	Store         storage.Store
	Notifier      machine.Notifier
}

// New creates a new App instance.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	logger := config.NewLogger()

	app := &App{
		Config: cfg,
		Logger: logger,
	}

	// Initialize storage
	var store storage.Store
	if cfg.DataDir != "" {
		fileStore, err := storage.NewFileStore(cfg.DataDir)
		if err != nil {
			return nil, errors.Wrap(err, "create file store")
		}
		store = fileStore
		logger.Info("using file-based storage", slog.String("data_dir", cfg.DataDir))
	} else {
		store = &storage.NullStore{}
		logger.Info("storage disabled, operations will not persist")
	}
	app.Store = store

	// Initialize ClientManager
	var clientManager *rds.ClientManager

	if cfg.DemoMode && cfg.RDSEndpoint != "" {
		// In demo mode, create a minimal AWS config that won't try to fetch real credentials
		awsCfg := aws.Config{
			Region:           cfg.AWSRegion,
			RetryMaxAttempts: 1, // Fail fast in demo mode
			Credentials:      aws.AnonymousCredentials{},
		}
		clientManager = rds.NewClientManager(rds.ClientManagerConfig{
			BaseConfig: awsCfg,
			DemoMode:   true,
			BaseURL:    cfg.RDSEndpoint,
		})
		logger.Info("using demo mode with mock RDS endpoint", slog.String("endpoint", cfg.RDSEndpoint))
	} else {
		// Normal mode - load AWS config from environment/profile
		awsCfg, err := cfg.LoadAWSConfig(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "load aws config")
		}

		clientManager = rds.NewClientManager(rds.ClientManagerConfig{
			BaseConfig: awsCfg,
			Profile:    cfg.AWSProfile,
		})
	}
	app.ClientManager = clientManager

	// Initialize notifier
	var notifier machine.Notifier
	if cfg.SlackEnabled && cfg.SlackToken != "" {
		notifier = notifiers.NewSlackNotifier(cfg.SlackToken, cfg.SlackChannel)
	} else {
		notifier = &notifiers.NullNotifier{}
	}
	app.Notifier = notifier

	// Initialize engine
	app.Engine = machine.NewEngine(machine.EngineConfig{
		ClientManager:       clientManager,
		Store:               store,
		Logger:              logger,
		Notifier:            notifier,
		DefaultRegion:       cfg.AWSRegion,
		DefaultWaitTimeout:  time.Duration(cfg.DefaultWaitTimeout) * time.Second,
		DefaultPollInterval: time.Duration(cfg.DefaultPollInterval) * time.Second,
	})

	// Load state from storage
	runningOps, err := app.Engine.LoadFromStore(ctx)
	if err != nil {
		logger.Warn("failed to load state from storage", slog.String("error", err.Error()))
	} else if len(runningOps) > 0 {
		// Resume or pause running operations based on config
		app.Engine.ResumeRunningOperations(ctx, runningOps, cfg.AutoResume)
	}

	return app, nil
}

// NewWithEngine creates an App with a pre-configured engine (for testing).
func NewWithEngine(cfg *config.Config, engine *machine.Engine, notifier machine.Notifier) *App {
	return &App{
		Config:   cfg,
		Logger:   config.NewLogger(),
		Engine:   engine,
		Notifier: notifier,
	}
}

// StatusResponse contains application status.
type StatusResponse struct {
	Status       string `json:"status"`
	SlackEnabled bool   `json:"slack_enabled"`
	Operations   struct {
		Total    int `json:"total"`
		Running  int `json:"running"`
		Paused   int `json:"paused"`
		Complete int `json:"complete"`
		Failed   int `json:"failed"`
	} `json:"operations"`
}

// GetStatus returns the current application status.
func (a *App) GetStatus() StatusResponse {
	ops := a.Engine.ListOperations()

	status := StatusResponse{
		Status:       "ok",
		SlackEnabled: a.Config.SlackEnabled,
	}
	status.Operations.Total = len(ops)

	for _, op := range ops {
		switch op.State {
		case types.StateRunning:
			status.Operations.Running++
		case types.StatePaused:
			status.Operations.Paused++
		case types.StateCompleted:
			status.Operations.Complete++
		case types.StateFailed, types.StateRolledBack:
			status.Operations.Failed++
		}
	}

	return status
}

// CreateOperationRequest is the request to create a new operation.
type CreateOperationRequest struct {
	Type        types.OperationType `json:"type"`
	ClusterID   string              `json:"cluster_id"`
	Region      string              `json:"region,omitempty"`
	Params      json.RawMessage     `json:"params"`
	WaitTimeout int                 `json:"wait_timeout,omitempty"` // seconds
}

// CreateOperation creates a new maintenance operation.
func (a *App) CreateOperation(ctx context.Context, req CreateOperationRequest) (*types.Operation, error) {
	return a.Engine.CreateOperation(ctx, req.Type, req.ClusterID, req.Region, req.Params, req.WaitTimeout)
}

// GetOperation returns an operation by ID.
func (a *App) GetOperation(id string) (*types.Operation, error) {
	return a.Engine.GetOperation(id)
}

// ListOperations returns all operations.
func (a *App) ListOperations() []*types.Operation {
	return a.Engine.ListOperations()
}

// GetEvents returns events for an operation.
func (a *App) GetEvents(operationID string) ([]types.Event, error) {
	return a.Engine.GetEvents(operationID)
}

// StartOperation starts an operation.
func (a *App) StartOperation(ctx context.Context, id string) error {
	return a.Engine.StartOperation(ctx, id)
}

// ResumeOperation resumes a paused operation.
func (a *App) ResumeOperation(ctx context.Context, id string, response types.InterventionResponse) error {
	return a.Engine.ResumeOperation(ctx, id, response)
}

// PauseOperation pauses a running operation.
func (a *App) PauseOperation(ctx context.Context, id string, reason string) error {
	return a.Engine.PauseOperation(ctx, id, reason)
}

// UpdateOperationTimeout updates the wait timeout for an operation.
func (a *App) UpdateOperationTimeout(ctx context.Context, id string, timeout int) error {
	return a.Engine.UpdateOperationTimeout(ctx, id, timeout)
}

// DeleteOperation deletes an operation that was created but never started.
func (a *App) DeleteOperation(ctx context.Context, id string) error {
	return a.Engine.DeleteOperation(ctx, id)
}

// DeleteAllOperations force-deletes all operations (for demo mode reset).
// Returns count of deleted operations and any errors encountered.
func (a *App) DeleteAllOperations(ctx context.Context) (int, []string) {
	ops := a.Engine.ListOperations()
	deleted := 0
	var errs []string

	for _, op := range ops {
		if err := a.Engine.ForceDeleteOperation(ctx, op.ID); err != nil {
			errs = append(errs, op.ID+": "+err.Error())
		} else {
			deleted++
		}
	}

	return deleted, errs
}

// ListRegions returns available AWS regions.
func (a *App) ListRegions(ctx context.Context) ([]string, error) {
	return a.ClientManager.ListRegions(ctx)
}

// ListClusters returns Aurora clusters in the specified region.
func (a *App) ListClusters(ctx context.Context, region string) ([]types.ClusterSummary, error) {
	client, err := a.ClientManager.GetClient(ctx, region)
	if err != nil {
		return nil, err
	}
	return client.ListClusters(ctx)
}

// GetClusterInfo returns detailed cluster information.
func (a *App) GetClusterInfo(ctx context.Context, region, clusterID string) (*types.ClusterInfo, error) {
	client, err := a.ClientManager.GetClient(ctx, region)
	if err != nil {
		return nil, err
	}
	return client.GetClusterInfo(ctx, clusterID)
}

// GetBlueGreenDeployments returns Blue-Green deployments for a cluster.
func (a *App) GetBlueGreenDeployments(ctx context.Context, region, clusterID string) ([]*rds.BlueGreenDeploymentInfo, error) {
	client, err := a.ClientManager.GetClient(ctx, region)
	if err != nil {
		return nil, err
	}
	// Get cluster ARN first
	clusterARN, err := client.GetClusterARN(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return client.ListBlueGreenDeploymentsForCluster(ctx, clusterARN)
}
