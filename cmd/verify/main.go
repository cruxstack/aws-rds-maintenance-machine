// Package main provides offline integration testing using HTTP mock servers.
// Tests RDS maintenance operations without requiring live AWS credentials.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

func main() {
	scenarioFile := flag.String("scenarios", "fixtures/scenarios.yaml", "path to test scenarios file")
	verbose := flag.Bool("verbose", false, "enable verbose output")
	scenarioFilter := flag.String("filter", "", "run only scenarios matching this name")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load .env.test file
	envPath := filepath.Join("cmd", "verify", ".env")
	envTestPath := filepath.Join("cmd", "verify", ".env.test")

	if _, err := os.Stat(envPath); err == nil {
		if err := godotenv.Load(envPath); err != nil {
			logger.Warn("failed to load .env file", slog.String("error", err.Error()))
		}
	} else if _, err := os.Stat(envTestPath); err == nil {
		fmt.Printf("Using .env.test (no .env file found)\n")
		if err := godotenv.Load(envTestPath); err != nil {
			logger.Warn("failed to load .env.test file", slog.String("error", err.Error()))
		}
	}

	ctx := context.Background()

	// Read scenarios
	path := filepath.Join(*scenarioFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		logger.Error("failed to read scenarios file", slog.String("error", err.Error()))
		os.Exit(1)
	}

	var scenarios []TestScenario
	if err := yaml.Unmarshal(raw, &scenarios); err != nil {
		logger.Error("failed to parse scenarios", slog.String("error", err.Error()))
		os.Exit(1)
	}

	passed := 0
	failed := 0
	skipped := 0

	for _, scenario := range scenarios {
		if *scenarioFilter != "" && !strings.Contains(scenario.Name, *scenarioFilter) {
			skipped++
			continue
		}

		if err := runScenario(ctx, scenario, *verbose, logger); err != nil {
			fmt.Printf("  FAILED: %v\n\n", err)
			failed++
		} else {
			passed++
		}
	}

	fmt.Printf("\n")
	separator := strings.Repeat("=", 60)
	fmt.Printf("%s\n", separator)
	if failed > 0 {
		fmt.Printf("  Test Results: %d passed, %d failed, %d skipped\n", passed, failed, skipped)
	} else {
		fmt.Printf("  Test Results: All %d tests passed, %d skipped\n", passed, skipped)
	}
	fmt.Printf("%s\n", separator)

	if failed > 0 {
		os.Exit(1)
	}
}
