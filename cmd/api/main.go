// Command api serves the public REST API of Uptime Monitor.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/config"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/observability"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/healthcheck"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/httpserver"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/logger"
)

// Build metadata, set by the linker (see LDFLAGS in the Makefile):
//
//	go build -ldflags "-X main.version=1.2.3 -X main.commit=abc -X main.builtAt=..."
var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

const service = "api"

// healthcheckTimeout is how long -healthcheck waits for /healthz.
const healthcheckTimeout = 3 * time.Second

func main() {
	info := observability.NewBuildInfo(version, commit, builtAt)
	if err := run(context.Background(), os.Args[1:], info, os.Stdout); err != nil {
		// The configured logger may not exist yet (e.g. config is invalid), so
		// the error is reported in the production format.
		log := logger.New(os.Stdout, logger.Options{
			Format:  logger.FormatJSON,
			Level:   slog.LevelInfo,
			Service: service,
			Version: info.Version,
		})
		log.Error("api failed", slog.Any("error", err))
		os.Exit(1)
	}
}

// run is main without os.Exit, so tests can call it. Logs go to stdout.
func run(ctx context.Context, args []string, info observability.BuildInfo, stdout io.Writer) error {
	flags := flag.NewFlagSet(service, flag.ContinueOnError)
	showVersion := flags.Bool("version", false, "print version information and exit")
	probe := flags.Bool("healthcheck", false, "probe /healthz of the running instance and exit (0 = healthy)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *showVersion {
		_, err := fmt.Fprintf(stdout, "%s %s (commit %s, built %s, %s)\n",
			service, info.Version, info.Commit, info.BuiltAt, info.GoVersion)
		return err
	}

	cfg, err := config.LoadAPI()
	if err != nil {
		return err
	}

	if *probe {
		return runHealthcheck(ctx, cfg.HTTPAddr)
	}
	return serve(ctx, cfg, info, stdout)
}

func runHealthcheck(ctx context.Context, listenAddr string) error {
	url, err := healthcheck.URL(listenAddr, "/healthz")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	return healthcheck.Check(ctx, url)
}

func serve(ctx context.Context, cfg config.API, info observability.BuildInfo, stdout io.Writer) error {
	log := logger.New(stdout, logger.Options{
		Format:  logFormat(cfg.Env),
		Level:   cfg.LogLevel,
		Service: service,
		Version: info.Version,
	})

	// SIGTERM is what Docker and Kubernetes send on stop; os.Interrupt is
	// Ctrl+C. Either one cancels ctx, which starts the graceful shutdown.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	log.Info("api starting",
		slog.String("env", string(cfg.Env)),
		slog.String("commit", info.Commit),
		slog.String("go_version", info.GoVersion),
	)

	router := httpapi.NewRouter(httpapi.Deps{BuildInfo: info})
	if err := httpserver.New("public", router, log, cfg.ShutdownTimeout).Serve(ctx, ln); err != nil {
		return err
	}

	log.Info("api stopped")
	return nil
}

func logFormat(env config.Env) logger.Format {
	if env == config.EnvProd {
		return logger.FormatJSON
	}
	return logger.FormatText
}
