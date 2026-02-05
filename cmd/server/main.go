// Package main provides the HTTP server entry point for the RDS maintenance machine.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/app"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/config"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/constants"
	"github.com/mpz/devops/tools/rds-maint-machine/internal/httputil"
)

var (
	appInst *app.App
	logger  *slog.Logger
)

func main() {
	// Load .env file if present
	godotenv.Load()

	logger = config.NewLogger()
	ctx := context.Background()

	cfg, err := config.NewConfig()
	if err != nil {
		logger.Error("config init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	appInst, err = app.New(ctx, cfg)
	if err != nil {
		logger.Error("app init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	handler := httputil.NewRequestHandler(appInst, logger)

	port := cfg.Port
	if port == "" {
		port = constants.DefaultHTTPPort
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  constants.DefaultReadTimeout,
		WriteTimeout: constants.DefaultWriteTimeout,
		IdleTimeout:  constants.DefaultIdleTimeout,
	}

	done := make(chan bool, 1)
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		logger.Info("server is shutting down")

		ctx, cancel := context.WithTimeout(context.Background(), constants.DefaultShutdownTimeout)
		defer cancel()

		srv.SetKeepAlivesEnabled(false)
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("server shutdown failed", slog.String("error", err.Error()))
		}
		close(done)
	}()

	if cfg.TLSEnabled && cfg.TLSCertPath != "" && cfg.TLSKeyPath != "" {
		logger.Info("server starting with TLS", slog.String("port", port))
		if err := srv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	} else {
		logger.Info("server starting", slog.String("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}

	<-done
	logger.Info("server stopped")
}
