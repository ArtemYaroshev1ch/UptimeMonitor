package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDSN = "postgres://uptime:secret@localhost:5432/uptime"

// unset marks a variable that withDSN must leave out of the environment.
const unset = "\x00unset"

// withDSN returns environ plus a valid DATABASE_URL, so error cases fail
// only for the reason they test. A value of unset removes the variable.
func withDSN(environ map[string]string) map[string]string {
	out := map[string]string{"DATABASE_URL": testDSN}
	for k, v := range environ {
		if v == unset {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

func TestLoadAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		environ map[string]string
		want    API
	}{
		{
			name:    "defaults",
			environ: map[string]string{"DATABASE_URL": testDSN},
			want: API{
				Common: Common{
					Env:             EnvDev,
					LogLevel:        slog.LevelInfo,
					ShutdownTimeout: 25 * time.Second,
				},
				DB:       Database{URL: testDSN, MaxConns: 10},
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
				"DATABASE_URL":     testDSN,
				"DB_MAX_CONNS":     "4",
			},
			want: API{
				Common: Common{
					Env:             EnvProd,
					LogLevel:        slog.LevelDebug,
					ShutdownTimeout: 5 * time.Second,
				},
				DB:       Database{URL: testDSN, MaxConns: 4},
				HTTPAddr: "127.0.0.1:9000",
			},
		},
		{
			name:    "log level is case-insensitive",
			environ: map[string]string{"LOG_LEVEL": "WARN", "DATABASE_URL": testDSN},
			want: API{
				Common: Common{
					Env:             EnvDev,
					LogLevel:        slog.LevelWarn,
					ShutdownTimeout: 25 * time.Second,
				},
				DB:       Database{URL: testDSN, MaxConns: 10},
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
				"DB_MAX_CONNS":     "0",
			},
			wantErr: []string{"APP_ENV", "SHUTDOWN_TIMEOUT", "HTTP_ADDR", "DB_MAX_CONNS"},
		},
		{
			name:    "database URL is required",
			environ: map[string]string{"DATABASE_URL": unset},
			wantErr: []string{"DATABASE_URL"},
		},
		{
			name:    "database URL must not be empty",
			environ: map[string]string{"DATABASE_URL": ""},
			wantErr: []string{"DATABASE_URL"},
		},
		{
			name:    "zero pool size",
			environ: map[string]string{"DB_MAX_CONNS": "0"},
			wantErr: []string{"DB_MAX_CONNS must be at least 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadAPI(withDSN(tt.environ))

			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

func TestLoadMigrate(t *testing.T) {
	t.Parallel()

	got, err := loadMigrate(map[string]string{"DATABASE_URL": testDSN, "LOG_LEVEL": "debug"})

	require.NoError(t, err)
	assert.Equal(t, Migrate{
		Common: Common{
			Env:             EnvDev,
			LogLevel:        slog.LevelDebug,
			ShutdownTimeout: 25 * time.Second,
		},
		DatabaseURL: testDSN,
	}, got)
}

func TestLoadMigrateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		environ map[string]string
		wantErr string
	}{
		{name: "database URL is required", environ: map[string]string{}, wantErr: "DATABASE_URL"},
		{
			name:    "common settings are validated",
			environ: map[string]string{"DATABASE_URL": testDSN, "APP_ENV": "staging"},
			wantErr: "APP_ENV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadMigrate(tt.environ)

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
