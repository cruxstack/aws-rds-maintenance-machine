package rds

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/cockroachdb/errors"
)

// ClientManager manages RDS clients for multiple regions.
// It lazily creates clients as needed and caches them for reuse.
type ClientManager struct {
	mu         sync.RWMutex
	clients    map[string]*Client
	baseConfig aws.Config
	profile    string
	demoMode   bool
	baseURL    string // for demo mode
}

// ClientManagerConfig contains configuration for the ClientManager.
type ClientManagerConfig struct {
	// BaseConfig is the base AWS configuration (used for credentials).
	BaseConfig aws.Config
	// Profile is the AWS profile to use (optional).
	Profile string
	// DemoMode indicates if we're running in demo mode.
	DemoMode bool
	// BaseURL is the mock server URL for demo mode.
	BaseURL string
}

// NewClientManager creates a new ClientManager.
func NewClientManager(cfg ClientManagerConfig) *ClientManager {
	return &ClientManager{
		clients:    make(map[string]*Client),
		baseConfig: cfg.BaseConfig,
		profile:    cfg.Profile,
		demoMode:   cfg.DemoMode,
		baseURL:    cfg.BaseURL,
	}
}

// GetClient returns an RDS client for the specified region.
// Clients are cached and reused.
func (m *ClientManager) GetClient(ctx context.Context, region string) (*Client, error) {
	// Check cache first
	m.mu.RLock()
	client, ok := m.clients[region]
	m.mu.RUnlock()
	if ok {
		return client, nil
	}

	// Create new client
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if client, ok := m.clients[region]; ok {
		return client, nil
	}

	var awsCfg aws.Config
	var err error

	if m.demoMode {
		// Demo mode: use anonymous credentials
		awsCfg = aws.Config{
			Region:           region,
			RetryMaxAttempts: 1,
			Credentials:      aws.AnonymousCredentials{},
		}
	} else {
		// Normal mode: load config for the region
		opts := []func(*awsconfig.LoadOptions) error{
			awsconfig.WithRegion(region),
		}
		if m.profile != "" {
			opts = append(opts, awsconfig.WithSharedConfigProfile(m.profile))
		}

		awsCfg, err = awsconfig.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return nil, errors.Wrapf(err, "load aws config for region %s", region)
		}
	}

	clientCfg := ClientConfig{
		AWSConfig: awsCfg,
	}
	if m.baseURL != "" {
		clientCfg.BaseURL = m.baseURL
	}

	client = NewClient(clientCfg)
	m.clients[region] = client

	return client, nil
}

// ListRegions returns the list of available AWS regions.
func (m *ClientManager) ListRegions(ctx context.Context) ([]string, error) {
	if m.demoMode {
		// In demo mode, return a static list
		return []string{
			"us-east-1",
			"us-east-2",
			"us-west-1",
			"us-west-2",
			"eu-west-1",
			"eu-west-2",
			"eu-central-1",
			"ap-northeast-1",
			"ap-southeast-1",
			"ap-southeast-2",
		}, nil
	}

	// Use EC2 DescribeRegions to get the list
	ec2Client := ec2.NewFromConfig(m.baseConfig)
	out, err := ec2Client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false), // Only enabled regions
	})
	if err != nil {
		return nil, errors.Wrap(err, "describe regions")
	}

	regions := make([]string, 0, len(out.Regions))
	for _, r := range out.Regions {
		if r.RegionName != nil {
			regions = append(regions, *r.RegionName)
		}
	}

	return regions, nil
}

// IsDemoMode returns whether the manager is in demo mode.
func (m *ClientManager) IsDemoMode() bool {
	return m.demoMode
}
