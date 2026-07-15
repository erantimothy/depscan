// Command depscan runs the dependency-scanning HTTP service.
//
// main() has exactly one job: wire dependencies together and manage the
// process lifecycle. It contains no business logic — that all lives in
// internal/. This separation is what lets every other package be unit
// tested without ever starting a real HTTP server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	if len(os.Args) > 1 {
		handled, err := runCLI(os.Args[1:])
		if handled {
			return err
		}
	}

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

// runCLI handles the local, HTTP-free scan command. With no command the
// application continues to run its HTTP service for backwards compatibility.
func runCLI(args []string) (bool, error) {
	if args[0] != "scan" {
		return false, nil
	}

	flags := flag.NewFlagSet("depscan scan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	rootFlag := flags.String("root", "", "repository or directory to scan")
	output := flags.String("output", "", "write JSON to this file instead of stdout")
	// Accept both `scan --output result.json /repo` and the more natural
	// `scan /repo --output result.json` form. The standard flag package stops
	// parsing at the first positional argument, so move a leading path after
	// the flags before parsing.
	scanArgs := args[1:]
	if len(scanArgs) > 0 && !strings.HasPrefix(scanArgs[0], "-") {
		scanArgs = append(scanArgs[1:], scanArgs[0])
	}
	if err := flags.Parse(scanArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return true, err
	}

	root := *rootFlag
	if root == "" && flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	if root == "" {
		return true, errors.New("scan requires a repository path (for example: depscan scan /path/to/repo)")
	}
	if flags.NArg() > 1 || (*rootFlag != "" && flags.NArg() > 0) {
		return true, errors.New("scan accepts only one repository path")
	}

	cfg, err := config.Load()
	if err != nil {
		return true, fmt.Errorf("loading config: %w", err)
	}
	parse := modfile.New()
	result, err := scanner.New(parse, scanner.WithMaxWorkers(cfg.MaxScanWorkers)).Scan(context.Background(), root)
	if err != nil {
		return true, fmt.Errorf("scanning %s: %w", root, err)
	}

	var writer io.Writer = os.Stdout
	var file *os.File
	if *output != "" {
		if dir := filepath.Dir(*output); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return true, fmt.Errorf("creating output directory: %w", err)
			}
		}
		file, err = os.Create(*output)
		if err != nil {
			return true, fmt.Errorf("creating output file: %w", err)
		}
		defer file.Close()
		writer = file
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return true, fmt.Errorf("writing scan result: %w", err)
	}
	if *output != "" {
		fmt.Fprintf(os.Stderr, "scan %s written to %s\n", result.ID, *output)
	}
	return true, nil
}
