package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	fitnessauth "github.com/timdumol/fitsync/internal/auth"
	"github.com/timdumol/fitsync/internal/config"
	"github.com/timdumol/fitsync/internal/db"
	"github.com/timdumol/fitsync/internal/fitnessrepo"
	"github.com/timdumol/fitsync/internal/httpapi"
	"github.com/timdumol/fitsync/internal/ingest"
	"github.com/timdumol/fitsync/internal/mcpserver"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage())
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "identities":
		return runIdentities(args[1:])
	case "healthcheck":
		return runHealthcheck()
	case "mcp-smoke":
		return runMCPReadSmoke(args[1:])
	case "exercise-normalization":
		return runExerciseNormalization(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage())
	}
}

func runServe(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	address := flags.String("addr", cfg.Address, "HTTP listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: fitnessd serve [--addr ADDRESS]")
	}
	cfg.Address = *address
	database, err := db.OpenAndMigrate(context.Background(), cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	oidc, err := fitnessauth.New(context.Background(), database, cfg.Issuer, cfg.Resource, nil, cfg.TrustedProxy)
	if err != nil {
		return err
	}
	defer oidc.Close()
	api := httpapi.NewAPI(database, httpapi.NewPocketIDAuthorizer(oidc))
	ingestHandler := ingest.NewHandler(database, cfg)
	ingestHandler.SetAuthenticator(oidc)
	api.SetGarminRoutes(ingestHandler)
	if err := ingestHandler.Reconcile(context.Background()); err != nil {
		return fmt.Errorf("reconcile archives: %w", err)
	}
	mcp, err := mcpserver.New(api, oidc)
	if err != nil {
		return err
	}
	server := httpapi.NewServerWithAuth(cfg, logger, api, ingestHandler, mcp.Handler(), cfg.Issuer)
	shutdownSignals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := ingestHandler.RunDaily(shutdownSignals); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("archive reconciliation stopped", "error", err)
		}
	}()

	logger.Info("fitnessd listening", "address", server.Addr)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownSignals.Done():
		if err := httpapi.Shutdown(server, cfg.ShutdownTimeout); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

func runMigrate(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databasePath := flags.String("db", cfg.DatabasePath, "SQLite database path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: fitnessd migrate [--db PATH]")
	}
	database, err := db.OpenAndMigrate(context.Background(), *databasePath)
	if err != nil {
		return err
	}
	return database.Close()
}

func runIdentities(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: fitnessd identities {list-pending|list|bind|approve|disable}")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := db.OpenAndMigrate(context.Background(), cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	admin := fitnessauth.NewAdmin(database)
	switch args[0] {
	case "list-pending":
		if len(args) != 1 {
			return errors.New("usage: fitnessd identities list-pending")
		}
		items, err := admin.ListPending(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(items)
	case "list":
		if len(args) != 1 {
			return errors.New("usage: fitnessd identities list")
		}
		items, err := admin.ListIdentities(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(items)
	case "bind":
		flags := flag.NewFlagSet("identities bind", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		subject := flags.String("subject", "", "Pocket ID subject")
		handle := flags.String("athlete", "", "athlete handle")
		username := flags.String("username", "", "Pocket ID username returned by the admin API")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *subject == "" || *username == "" || (*handle != "tim" && *handle != "jopi") {
			return errors.New("usage: fitnessd identities bind --subject ID --username USERNAME --athlete tim|jopi")
		}
		if err := admin.Bind(context.Background(), cfg.Issuer, *subject, *username, *handle); err != nil {
			return err
		}
		fmt.Println("bound")
		return nil
	case "approve":
		flags := flag.NewFlagSet("identities approve", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		pendingID := flags.String("pending-id", "", "pending identity ID")
		handle := flags.String("athlete", "", "athlete handle")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *pendingID == "" || (*handle != "tim" && *handle != "jopi") {
			return errors.New("usage: fitnessd identities approve --pending-id ID --athlete tim|jopi")
		}
		if err := admin.Approve(context.Background(), *pendingID, *handle); err != nil {
			return err
		}
		fmt.Println("approved")
		return nil
	case "disable":
		flags := flag.NewFlagSet("identities disable", flag.ContinueOnError)
		flags.SetOutput(os.Stderr)
		handle := flags.String("athlete", "", "athlete handle")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || (*handle != "tim" && *handle != "jopi") {
			return errors.New("usage: fitnessd identities disable --athlete tim|jopi")
		}
		if err := admin.Disable(context.Background(), *handle); err != nil {
			return err
		}
		fmt.Println("disabled")
		return nil
	default:
		return errors.New("usage: fitnessd identities {list-pending|list|bind|approve|disable}")
	}
}

func runHealthcheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	var one int
	if err := database.QueryRow("SELECT 1").Scan(&one); err != nil {
		return err
	}
	if one != 1 {
		return errors.New("database health check failed")
	}
	return nil
}

func runMCPReadSmoke(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("mcp-smoke", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databasePath := flags.String("db", cfg.DatabasePath, "SQLite database path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: fitnessd mcp-smoke [--db PATH]")
	}
	database, err := db.Open(*databasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	athletes, err := mcpserver.RunProductionReadSmoke(context.Background(), database, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("MCP read smoke passed for %d production athlete(s); all read-only actions validated; write actions were not invoked.\n", athletes)
	return nil
}

func runExerciseNormalization(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("exercise-normalization", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	databasePath := flags.String("db", cfg.DatabasePath, "SQLite database path")
	apply := flags.Bool("apply", false, "apply exact reviewed aliases")
	backupPath := flags.String("backup", "", "absolute SQLite backup path required with --apply")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: fitnessd exercise-normalization [--db PATH] [--apply --backup ABSOLUTE_PATH]")
	}
	if *apply && strings.TrimSpace(*backupPath) == "" {
		return errors.New("usage: fitnessd exercise-normalization --apply --backup ABSOLUTE_PATH")
	}
	database, err := db.Open(*databasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	report, err := fitnessrepo.BackfillExerciseIdentities(context.Background(), database, *apply, *backupPath)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

func usage() string {
	return "usage: fitnessd {serve|migrate|identities {list-pending|list|bind|approve|disable}|healthcheck|mcp-smoke|exercise-normalization}"
}
