package middleware

import "net/http"

// MaxBodyBytes is the request body limit of the public API (1 MiB).
const MaxBodyBytes = 1 << 20

// BodyLimit caps the request body at n bytes. Reading past the limit fails
// with *http.MaxBytesError (handlers report it as a validation error) and
// makes the server close the connection, so a client cannot stream an
// endless body into memory.
func BodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
