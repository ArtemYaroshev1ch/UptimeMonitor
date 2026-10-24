// Package config loads service configuration from environment variables and
// validates it at startup, so a misconfigured process fails fast instead of
// misbehaving later.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/caarlos0/env/v11"
)

// Env is the deployment environment. It selects the log format.
type Env string

// Supported deployment environments.
const (
	EnvDev  Env = "dev"
	EnvProd Env = "prod"
)

// Common holds settings shared by all binaries.
type Common struct {
	Env             Env           `env:"APP_ENV"          envDefault:"dev"`
	LogLevel        slog.Level    `env:"LOG_LEVEL"        envDefault:"info"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"25s"`
}

// API is the configuration of cmd/api.
type API struct {
	Common

	HTTPAddr string `env:"HTTP_ADDR" envDefault:":8080"`
}

// LoadAPI reads the API configuration from the process environment.
func LoadAPI() (API, error) {
	return loadAPI(nil)
}

// loadAPI parses environ, or the process environment when environ is nil.
// Tests pass a map instead of mutating the real environment.
func loadAPI(environ map[string]string) (API, error) {
	cfg, err := env.ParseAsWithOptions[API](env.Options{Environment: environ})
	if err != nil {
		return API{}, fmt.Errorf("config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return API{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// validate checks values that parse fine but are unusable. It reports all
// problems at once, so they can be fixed in one go.
func (c Common) validate() error {
	var errs []error
	if c.Env != EnvDev && c.Env != EnvProd {
		errs = append(errs, fmt.Errorf("APP_ENV must be %q or %q, got %q", EnvDev, EnvProd, c.Env))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT must be positive, got %s", c.ShutdownTimeout))
	}
	return errors.Join(errs...)
}

func (c API) validate() error {
	errs := []error{c.Common.validate()}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("HTTP_ADDR must be host:port, got %q: %w", c.HTTPAddr, err))
	}
	return errors.Join(errs...)
}
