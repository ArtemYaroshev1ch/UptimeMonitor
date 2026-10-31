// Package logger builds the structured logger shared by all binaries.
package logger

import (
	"io"
	"log/slog"
)

// Format selects the log encoding.
type Format int

// Supported log formats.
const (
	// FormatText is human-readable key=value output for local development.
	FormatText Format = iota
	// FormatJSON writes one JSON object per line for log collectors.
	FormatJSON
)

// Options configures New.
type Options struct {
	Format  Format
	Level   slog.Leveler
	Service string // "api" or "worker"
	Version string
}

// New returns a logger that writes to w. Every record carries the service
// name and version, as the logging contract with uptime-infra requires; time,
// level and msg are added by slog itself. Attributes stored in the context
// with WithAttrs (request_id) are added to records logged with that context.
func New(w io.Writer, opts Options) *slog.Logger {
	handlerOpts := &slog.HandlerOptions{Level: opts.Level}

	var h slog.Handler
	if opts.Format == FormatJSON {
		h = slog.NewJSONHandler(w, handlerOpts)
	} else {
		h = slog.NewTextHandler(w, handlerOpts)
	}

	return slog.New(contextHandler{h}).With(
		slog.String("service", opts.Service),
		slog.String("version", opts.Version),
	)
}
