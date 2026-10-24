// Package response writes HTTP responses in the API's JSON format.
package response

import (
	"encoding/json"
	"net/http"
)

// JSON writes v as a JSON body with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// Only unsupported types (a programming error) fail to marshal.
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// A write error means the client has gone away; there is nobody to tell.
	_, _ = w.Write(append(body, '\n'))
}
