// Command depscan runs the dependency-scanning HTTP service.
//
// main() has exactly one job: wire dependencies together and manage the
// process lifecycle. It contains no business logic — that all lives in
// internal/. This separation is what lets every other package be unit
// tested without ever starting a real HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erantimothy/depscan/internal/api"
	"github.com/erantimothy/depscan/internal/config"
	"github.com/erantimothy/depscan/internal/github"
	"github.com/erantimothy/depscan/internal/logging"
	"github.com/erantimothy/depscan/internal/modfile"
	"github.com/erantimothy/depscan/internal/scanner"
	"github.com/erantimothy/depscan/internal/store"
)

func main() {
	if err := run(); err != nil {
		// A bare log.Fatal or fmt.Println here would be fine for a toy
		// script; in an enterprise service, exiting with a non-zero
		// status matters because it's how orchestrators (systemd,
		// Kubernetes, CI) detect failure.
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// run holds everything main() would otherwise do, but returns an error
// instead of exiting directly. This is a small but high-value pattern:
// run() itself is now testable (call it from a test with a fake
// listener/timeout), whereas a main() that calls os.Exit never is.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger) // so any stdlib code using the default logger matches our format

	// Dependency wiring: concrete implementations built here, then
	// injected into consumers as their interface types. This is the only
	// place in the whole program that knows modfile.Parser,
	// scanner.Scanner, store.MemoryStore, and github.Fetcher are the
	// concrete choices — everywhere else only sees domain.Parser /
	// domain.Scanner / domain.Store / domain.RepoFetcher.
	parser := modfile.New()
	scan := scanner.New(parser, scanner.WithMaxWorkers(cfg.MaxScanWorkers))
	repo := store.NewMemoryStore()
	fetcher := github.New()

	handler := api.NewServer(scan, repo, fetcher, logger)
	wrapped := api.Chain(handler,
		api.LoggingMiddleware(logger),
		api.RecoveryMiddleware(logger),
	)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: wrapped,

		// Enterprise-nonnegotiable HTTP server timeouts. Without these,
		// a slow or malicious client can hold a connection open
		// indefinitely (Slowloris-style) and exhaust server resources.
		// The zero-value http.Server has none of these set.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// signal.NotifyContext gives us a context that's cancelled the
	// moment the process receives SIGINT/SIGTERM — the same signals a
	// container orchestrator sends before killing a pod. Everything
	// downstream (in-flight scans, for instance) can watch this context
	// and wind down cleanly instead of being killed mid-operation.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server_starting", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	case <-ctx.Done():
		logger.Info("shutdown_signal_received")

		// A *separate* timeout context for shutdown itself: ctx is
		// already cancelled at this point (that's what triggered this
		// branch), so shutdown needs its own deadline rather than
		// inheriting the cancelled one.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		logger.Info("shutdown_complete")
	}

	return nil
}
