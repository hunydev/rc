package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
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
// Also accepts valid attach tokens (short-lived, one-time-use).
func checkAuth(r *http.Request, passwordHash string) bool {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(passwordHash)) == 1 {
			return true
		}
		// Check if it's an attach token
		if checkAttachToken(token) {
			return true
		}
	}
	// WebSocket subprotocol auth (browser cannot set Authorization header on WS)
	for _, proto := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		proto = strings.TrimSpace(proto)
		if strings.HasPrefix(proto, "auth-") {
			token := strings.TrimPrefix(proto, "auth-")
			if subtle.ConstantTimeCompare([]byte(token), []byte(passwordHash)) == 1 {
				return true
			}
			if checkAttachToken(token) {
				return true
			}
		}
	}
	return false
}

// ───── Attach tokens (short-lived tokens for agent --attach) ─────

const attachTokenExpiry = 5 * time.Minute

type attachTokenStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time // token hash → expiry
}

var attachTokens = &attachTokenStore{tokens: make(map[string]time.Time)}

// generateAttachToken creates a random short-lived token that can be used
// in place of the password for agent --attach. Returns the raw token string.
func generateAttachToken() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		// fallback to timestamp-based token
		b = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	token := hex.EncodeToString(b)
	hashed := hashPassword(token)
	attachTokens.mu.Lock()
	attachTokens.tokens[hashed] = time.Now().Add(attachTokenExpiry)
	attachTokens.mu.Unlock()
	// Prune expired tokens in background
	go attachTokens.prune()
	return token
}

// checkAttachToken validates a Bearer token against active attach tokens.
func checkAttachToken(tokenHash string) bool {
	attachTokens.mu.Lock()
	defer attachTokens.mu.Unlock()
	expiry, ok := attachTokens.tokens[tokenHash]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(attachTokens.tokens, tokenHash)
		return false
	}
	// One-time use: delete after successful validation
	delete(attachTokens.tokens, tokenHash)
	return true
}

func (s *attachTokenStore) prune() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.tokens {
		if now.After(exp) {
			delete(s.tokens, k)
		}
	}
}

// ───── Login rate limiter (progressive IP-based lockout) ─────

// lockout tiers: after N cumulative failures, lock for the given duration.
// Each tier is checked in order; the last matching tier applies.
var lockoutTiers = []struct {
	failures int
	duration time.Duration
}{
	{5, 5 * time.Minute},
	{10, 1 * time.Hour},
	{20, 24 * time.Hour},
}

const loginDelay = 500 * time.Millisecond

type ipRecord struct {
	failures  int
	lockedUntil time.Time
}

type loginRateLimiter struct {
	mu      sync.Mutex
	records map[string]*ipRecord
}

func newLoginRateLimiter() *loginRateLimiter {
	rl := &loginRateLimiter{records: make(map[string]*ipRecord)}
	// Periodically clean up expired records
	go func() {
		for range time.Tick(10 * time.Minute) {
			rl.mu.Lock()
			now := time.Now()
			for ip, rec := range rl.records {
				if rec.failures == 0 || now.After(rec.lockedUntil.Add(24*time.Hour)) {
					delete(rl.records, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

func (rl *loginRateLimiter) extractIP(r *http.Request) string {
	// Only trust proxy headers when --trusted-proxy is enabled
	if trustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.SplitN(xff, ",", 2)
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
		if xff := r.Header.Get("X-Real-Ip"); xff != "" {
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// check returns remaining lockout duration (0 = not locked)
func (rl *loginRateLimiter) check(ip string) time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rec, ok := rl.records[ip]
	if !ok {
		return 0
	}
	remaining := time.Until(rec.lockedUntil)
	if remaining > 0 {
		return remaining
	}
	return 0
}

func (rl *loginRateLimiter) recordFailure(ip string) time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rec, ok := rl.records[ip]
	if !ok {
		rec = &ipRecord{}
		rl.records[ip] = rec
	}
	rec.failures++
	// Apply the highest matching lockout tier
	var lockDuration time.Duration
	for _, tier := range lockoutTiers {
		if rec.failures >= tier.failures {
			lockDuration = tier.duration
		}
	}
	if lockDuration > 0 {
		rec.lockedUntil = time.Now().Add(lockDuration)
	}
	return lockDuration
}

func (rl *loginRateLimiter) recordSuccess(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.records, ip)
}

// handleLogin returns an HTTP handler for the /login endpoint.
// Accepts POST with JSON body {"password":"..."}.
// Applies a fixed delay + progressive IP-based rate limiting.
func handleLogin(password string, rl *loginRateLimiter) http.HandlerFunc {
	if password == "" {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"token":""}`))
		}
	}
	hashed := hashPassword(password)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ip := rl.extractIP(r)

		// Check if IP is currently locked out
		if remaining := rl.check(ip); remaining > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remaining.Seconds())+1))
			w.WriteHeader(http.StatusTooManyRequests)
			secs := int(remaining.Seconds())
			if secs < 1 {
				secs = 1
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":       "too_many_attempts",
				"retry_after": secs,
			})
			return
		}

		// Anti brute-force delay
		time.Sleep(loginDelay)

		var body struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_request"}`))
			return
		}

		inputHash := hashPassword(body.Password)
		if subtle.ConstantTimeCompare([]byte(inputHash), []byte(hashed)) != 1 {
			lockDuration := rl.recordFailure(ip)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			resp := map[string]interface{}{"error": "invalid_password"}
			if lockDuration > 0 {
				secs := int(lockDuration.Seconds())
				resp["locked_for"] = secs
				log.Printf("Login: IP %s locked for %v after failed attempt", ip, lockDuration)
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Success — clear failures and return token
		rl.recordSuccess(ip)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": hashed})
	}
}
