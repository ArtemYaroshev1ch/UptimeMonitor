package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(&buf, Options{
		Format:  FormatJSON,
		Level:   slog.LevelInfo,
		Service: "api",
		Version: "1.2.3",
	})

	log.Info("server started", "addr", ":8080")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record), "output must be a JSON object: %s", buf.String())
	for _, key := range []string{"time", "level", "msg", "service", "version"} {
		assert.Contains(t, record, key, "contract field %q is missing", key)
	}
	assert.Equal(t, "INFO", record["level"])
	assert.Equal(t, "server started", record["msg"])
	assert.Equal(t, "api", record["service"])
	assert.Equal(t, "1.2.3", record["version"])
	assert.Equal(t, ":8080", record["addr"])
}

func TestNewText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(&buf, Options{
		Format:  FormatText,
		Level:   slog.LevelInfo,
		Service: "worker",
		Version: "dev",
	})

	log.Info("hello")

	out := buf.String()
	assert.False(t, json.Valid(buf.Bytes()), "text format must not be JSON")
	assert.Contains(t, out, "msg=hello")
	assert.Contains(t, out, "service=worker")
	assert.Contains(t, out, "version=dev")
}

func TestNewLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := New(&buf, Options{Format: FormatJSON, Level: slog.LevelWarn})

	log.Info("filtered out")
	assert.Zero(t, buf.Len(), "records below the level must be dropped")

	log.Warn("kept")
	assert.Contains(t, buf.String(), "kept")
}
