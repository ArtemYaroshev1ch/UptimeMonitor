package response

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()

	JSON(rec, http.StatusCreated, map[string]string{"status": "ok"})

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestJSONMarshalError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()

	JSON(rec, http.StatusOK, make(chan int)) // channels cannot be marshaled

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Header().Get("Content-Type"), "application/json")
}
