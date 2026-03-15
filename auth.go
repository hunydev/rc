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
// Supports Authorization: Bearer <token> header and Sec-WebSocket-Protocol: auth-<token>.
func checkAuth(r *http.Request, password string) bool {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		if strings.TrimPrefix(auth, "Bearer ") == password {
			return true
		}
	}
	// WebSocket subprotocol auth (browser cannot set Authorization header on WS)
	for _, proto := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		proto = strings.TrimSpace(proto)
		if strings.HasPrefix(proto, "auth-") && strings.TrimPrefix(proto, "auth-") == password {
			return true
		}
	}
	return false
}
