package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	pubB64 := os.Getenv("ACP_INSTITUTION_PUBLIC_KEY")
	prvB64 := os.Getenv("ACP_INSTITUTION_PRIVATE_KEY")

	if pubB64 == "" || prvB64 == "" {
		log.Fatal("ACP_INSTITUTION_PUBLIC_KEY and ACP_INSTITUTION_PRIVATE_KEY env vars are required.\n" +
			"Generate with: go run ./cmd/keygen")
	}

	pubBytes, err := base64.RawURLEncoding.DecodeString(pubB64)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		log.Fatalf("ACP_INSTITUTION_PUBLIC_KEY must be a base64url-encoded 32-byte Ed25519 public key: %v", err)
	}
	prvBytes, err := base64.RawURLEncoding.DecodeString(prvB64)
	if err != nil || len(prvBytes) != ed25519.PrivateKeySize {
		log.Fatalf("ACP_INSTITUTION_PRIVATE_KEY must be a base64url-encoded 64-byte Ed25519 private key: %v", err)
	}

	store := NewStore()
	srv := NewServer(store, ed25519.PublicKey(pubBytes), ed25519.PrivateKey(prvBytes))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /acp/v1/health", srv.handleHealth)
	mux.HandleFunc("GET /acp/v1/challenge", srv.handleChallenge)
	mux.HandleFunc("POST /acp/v1/verify", srv.handleVerify)
	mux.HandleFunc("POST /acp/v1/agents", srv.handleRegisterAgent)
	mux.HandleFunc("GET /acp/v1/agents", srv.handleListAgents)
	mux.HandleFunc("POST /acp/v1/tokens", srv.handleIssueToken)
	mux.HandleFunc("POST /acp/v1/exec-tokens/{id}/consume", srv.handleConsumeETPattern)
	mux.HandleFunc("POST /acp/v1/policy-snapshots", srv.handleCreatePolicy)
	mux.HandleFunc("GET /acp/v1/policy-snapshots/active", srv.handleGetActivePolicy)

	addr := fmt.Sprintf(":%s", getEnvOr("ACP_PORT", "8080"))
	log.Printf("ACP admission server listening on %s", addr)
	if err := http.ListenAndServe(addr, loggingMiddleware(mux)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// handleConsumeETPattern is the Go 1.22 pattern-matched route handler for ET consumption.
func (s *Server) handleConsumeETPattern(w http.ResponseWriter, r *http.Request) {
	etID := r.PathValue("id")
	if etID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ET_ID", "execution token ID required in path")
		return
	}
	et, err := s.store.ConsumeExecutionToken(etID)
	if err != nil {
		writeError(w, http.StatusNotFound, "ET_NOT_FOUND", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"et_id":       et.ETID,
		"consumed":    true,
		"consumed_at": et.ConsumedAt,
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
