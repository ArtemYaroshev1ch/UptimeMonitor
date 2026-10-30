package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// blockingDB is a Pinger that hangs until the context ends, like a database
// behind a dropped network.
type blockingDB struct{}

func (blockingDB) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// A hanging database must not hang the probe: the check has its own timeout.
// The request context is already cancelled here, which makes the test fast
// while exercising the same path.
func TestReadyzDoesNotHang(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/readyz", http.NoBody)
	rec := httptest.NewRecorder()

	Readyz(blockingDB{}, slog.New(slog.NewTextHandler(io.Discard, nil)))(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
