package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// hashPassword returns the SHA-256 hex digest of the given password.
func hashPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return hex.EncodeToString(h[:])
}

// requireAuth wraps an HTTP handler with Bearer token authentication.
// If password is empty, no authentication is required.
// The token is compared against the SHA-256 hash of the configured password.
func requireAuth(password string, next http.HandlerFunc) http.HandlerFunc {
	if password == "" {
		return next
	}
	hashed := hashPassword(password)
	return func(w http.ResponseWriter, r *http.Request) {
		if checkAuth(r, hashed) {
			next(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}
}

// checkAuth validates the request token against the password hash.
// Supports Authorization: Bearer <hash> header and Sec-WebSocket-Protocol: auth-<hash>.
func checkAuth(r *http.Request, passwordHash string) bool {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		if strings.TrimPrefix(auth, "Bearer ") == passwordHash {
			return true
		}
	}
	// WebSocket subprotocol auth (browser cannot set Authorization header on WS)
	for _, proto := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		proto = strings.TrimSpace(proto)
		if strings.HasPrefix(proto, "auth-") && strings.TrimPrefix(proto, "auth-") == passwordHash {
			return true
		}
	}
	return false
}
