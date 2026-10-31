package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(&buf, Options{Format: FormatJSON, Level: slog.LevelInfo, Service: "api", Version: "1.2.3"})

	ctx := WithAttrs(t.Context(), slog.String("request_id", "req-1"))
	ctx = WithAttrs(ctx, slog.String("user_id", "u-1"))
	log.With(slog.String("component", "test")).InfoContext(ctx, "hello")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record), buf.String())
	assert.Equal(t, "req-1", record["request_id"])
	assert.Equal(t, "u-1", record["user_id"])
	assert.Equal(t, "test", record["component"])
	assert.Equal(t, "api", record["service"])
}

func TestWithAttrsDoesNotLeakIntoParent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(&buf, Options{Format: FormatJSON, Level: slog.LevelInfo})

	parent := WithAttrs(context.Background(), slog.String("a", "1"))
	_ = WithAttrs(parent, slog.String("b", "2"))
	log.InfoContext(parent, "hello")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record), buf.String())
	assert.Equal(t, "1", record["a"])
	assert.NotContains(t, record, "b")
}
