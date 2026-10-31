package logger

import (
	"context"
	"log/slog"
)

type attrsKey struct{}

// WithAttrs returns a copy of ctx carrying attrs. Every record logged with
// that context (log.InfoContext(ctx, ...)) gets them, so a request ID set
// once by middleware appears in all logs of the request, including logs
// written deep inside services that only see ctx.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	prev := attrsFrom(ctx)
	merged := make([]slog.Attr, 0, len(prev)+len(attrs))
	merged = append(merged, prev...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, attrsKey{}, merged)
}

func attrsFrom(ctx context.Context) []slog.Attr {
	attrs, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	return attrs
}

// contextHandler adds the attributes stored by WithAttrs to each record.
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs := attrsFrom(ctx); len(attrs) > 0 {
		r = r.Clone()
		r.AddAttrs(attrs...)
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
