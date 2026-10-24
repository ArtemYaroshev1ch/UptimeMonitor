package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		environ map[string]string
		want    API
	}{
		{
			name:    "defaults",
			environ: map[string]string{},
			want: API{
				Common: Common{
					Env:             EnvDev,
					LogLevel:        slog.LevelInfo,
					ShutdownTimeout: 25 * time.Second,
				},
				HTTPAddr: ":8080",
			},
		},
		{
			name: "overrides",
			environ: map[string]string{
				"APP_ENV":          "prod",
				"LOG_LEVEL":        "debug",
				"SHUTDOWN_TIMEOUT": "5s",
				"HTTP_ADDR":        "127.0.0.1:9000",
			},
			want: API{
				Common: Common{
					Env:             EnvProd,
					LogLevel:        slog.LevelDebug,
					ShutdownTimeout: 5 * time.Second,
				},
				HTTPAddr: "127.0.0.1:9000",
			},
		},
		{
			name:    "log level is case-insensitive",
			environ: map[string]string{"LOG_LEVEL": "WARN"},
			want: API{
				Common: Common{
					Env:             EnvDev,
					LogLevel:        slog.LevelWarn,
					ShutdownTimeout: 25 * time.Second,
				},
				HTTPAddr: ":8080",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := loadAPI(tt.environ)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadAPIErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		environ map[string]string
		// wantErr lists substrings that must all appear in the error.
		wantErr []string
	}{
		{
			name:    "unknown environment",
			environ: map[string]string{"APP_ENV": "staging"},
			wantErr: []string{"APP_ENV"},
		},
		// Parse errors come from caarlos0/env, which names the struct field
		// rather than the variable, along with the rejected value.
		{
			name:    "unknown log level",
			environ: map[string]string{"LOG_LEVEL": "verbose"},
			wantErr: []string{"LogLevel", "verbose"},
		},
		{
			name:    "malformed duration",
			environ: map[string]string{"SHUTDOWN_TIMEOUT": "soon"},
			wantErr: []string{"ShutdownTimeout", "soon"},
		},
		{
			name:    "zero shutdown timeout",
			environ: map[string]string{"SHUTDOWN_TIMEOUT": "0s"},
			wantErr: []string{"SHUTDOWN_TIMEOUT must be positive"},
		},
		{
			name:    "listen address without port",
			environ: map[string]string{"HTTP_ADDR": "8080"},
			wantErr: []string{"HTTP_ADDR must be host:port"},
		},
		{
			name: "all validation errors are reported together",
			environ: map[string]string{
				"APP_ENV":          "staging",
				"SHUTDOWN_TIMEOUT": "-1s",
				"HTTP_ADDR":        "localhost",
			},
			wantErr: []string{"APP_ENV", "SHUTDOWN_TIMEOUT", "HTTP_ADDR"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadAPI(tt.environ)

			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}
