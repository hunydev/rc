package main

import (
	"net/http"
	"strings"
)

// requireAuth wraps an HTTP handler with Bearer token authentication.
// If password is empty, no authentication is required.
func requireAuth(password string, next http.HandlerFunc) http.HandlerFunc {
	if password == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if checkAuth(r, password) {
			next(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}
}

// checkAuth validates the request against the given password.
// Supports Authorization: Bearer <token> header and ?token=<token> query param.
func checkAuth(r *http.Request, password string) bool {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		if strings.TrimPrefix(auth, "Bearer ") == password {
			return true
		}
	}
	if r.URL.Query().Get("token") == password {
		return true
	}
	return false
}
