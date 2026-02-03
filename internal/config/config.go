// Package config provides configuration loading for the RDS maintenance machine.
package config

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// Config holds all configuration for the application.
type Config struct {
	// Server configuration
	Port     string
	BasePath string

	// AWS configuration
	AWSRegion  string
	AWSProfile string

	// RDS endpoint override (for demo/testing with mock server)
	RDSEndpoint string

	// Slack configuration
	SlackEnabled bool
	SlackToken   string
	SlackChannel string

	// Admin configuration
	AdminToken string

	// Debug settings
	DebugEnabled bool

	// TLS configuration for server
	TLSEnabled  bool
	TLSCertPath string
	TLSKeyPath  string

	// Operation settings
	DefaultWaitTimeout  int // seconds
	DefaultPollInterval int // seconds

	// Storage settings
	DataDir    string // directory for persistent storage
	AutoResume bool   // automatically resume running operations on startup

	// Demo mode settings
	DemoMode     bool
	MockEndpoint string // URL of mock RDS server for demo mode

	// Experimental features
	ExperimentalStepFnEnabled bool // Enable experimental Step Functions support (disabled by default)
}

// NewConfig creates a new Config from environment variables.
func NewConfig() (*Config, error) {
	cfg := &Config{
		Port:                      getEnv("APP_PORT", "3000"),
		BasePath:                  getEnv("APP_BASE_PATH", ""),
		AWSRegion:                 getEnv("AWS_REGION", "us-east-1"),
		AWSProfile:                getEnv("AWS_PROFILE", ""),
		RDSEndpoint:               getEnv("RDS_ENDPOINT", ""),
		SlackEnabled:              getEnvBool("APP_SLACK_ENABLED", false),
		SlackToken:                getEnv("APP_SLACK_TOKEN", ""),
		SlackChannel:              getEnv("APP_SLACK_CHANNEL", ""),
		AdminToken:                getEnv("APP_ADMIN_TOKEN", ""),
		DebugEnabled:              getEnvBool("APP_DEBUG_ENABLED", false),
		TLSEnabled:                getEnvBool("APP_TLS_ENABLED", false),
		TLSCertPath:               getEnv("APP_TLS_CERT_PATH", ""),
		TLSKeyPath:                getEnv("APP_TLS_KEY_PATH", ""),
		DefaultWaitTimeout:        getEnvInt("APP_DEFAULT_WAIT_TIMEOUT", 2700), // 45 minutes
		DefaultPollInterval:       getEnvInt("APP_DEFAULT_POLL_INTERVAL", 30),  // 30 seconds
		DataDir:                   getEnv("APP_DATA_DIR", "./data"),
		AutoResume:                getEnvBool("APP_AUTO_RESUME", true), // default to auto-resume
		DemoMode:                  getEnvBool("APP_DEMO_MODE", false),
		MockEndpoint:              getEnv("APP_MOCK_ENDPOINT", ""),
		ExperimentalStepFnEnabled: getEnvBool("APP_EXPERIMENTAL_STEPFN_ENABLED", false),
	}

	if cfg.SlackToken != "" {
		cfg.SlackEnabled = true
	}

	return cfg, nil
}

// LoadAWSConfig loads the AWS SDK configuration.
func (c *Config) LoadAWSConfig(ctx context.Context) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(c.AWSRegion),
	}

	if c.AWSProfile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(c.AWSProfile))
	}

	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

// Redacted returns a copy of the config with sensitive values redacted.
func (c *Config) Redacted() map[string]any {
	return map[string]any{
		"port":                        c.Port,
		"base_path":                   c.BasePath,
		"aws_region":                  c.AWSRegion,
		"aws_profile":                 c.AWSProfile,
		"rds_endpoint":                c.RDSEndpoint,
		"slack_enabled":               c.SlackEnabled,
		"slack_token":                 redact(c.SlackToken),
		"slack_channel":               c.SlackChannel,
		"admin_token":                 redact(c.AdminToken),
		"debug_enabled":               c.DebugEnabled,
		"tls_enabled":                 c.TLSEnabled,
		"default_wait_timeout":        c.DefaultWaitTimeout,
		"default_poll_interval":       c.DefaultPollInterval,
		"data_dir":                    c.DataDir,
		"auto_resume":                 c.AutoResume,
		"demo_mode":                   c.DemoMode,
		"mock_endpoint":               c.MockEndpoint,
		"experimental_stepfn_enabled": c.ExperimentalStepFnEnabled,
	}
}

// NewLogger creates a new structured logger.
func NewLogger() *slog.Logger {
	level := slog.LevelInfo
	if getEnvBool("APP_DEBUG_ENABLED", false) {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return strings.ToLower(value) == "true" || value == "1"
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "***" + s[len(s)-4:]
}
