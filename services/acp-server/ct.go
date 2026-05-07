package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// b64 is the URL-safe base64 encoding without padding used throughout.
var b64 = base64.RawURLEncoding

// SignCT signs a capability token payload with the institution's private key.
// Returns a dot-separated base64url string: header.payload.signature
func SignCT(payload CTPayload, privKey ed25519.PrivateKey) (string, error) {
	header := CTHeader{Alg: "Ed25519", Typ: "ACP-CT-1.0"}

	hBytes, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}
	pBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	hEnc := b64.EncodeToString(hBytes)
	pEnc := b64.EncodeToString(pBytes)
	sigInput := hEnc + "." + pEnc

	sig := ed25519.Sign(privKey, []byte(sigInput))
	sEnc := b64.EncodeToString(sig)

	return hEnc + "." + pEnc + "." + sEnc, nil
}

// VerifyCT validates a CT string against the institution's public key.
// Returns the parsed payload if valid.
func VerifyCT(token string, pubKey ed25519.PublicKey) (*CTPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid CT format: expected 3 parts, got %d", len(parts))
	}

	hEnc, pEnc, sEnc := parts[0], parts[1], parts[2]
	sigInput := hEnc + "." + pEnc

	sig, err := b64.DecodeString(sEnc)
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(pubKey, []byte(sigInput), sig) {
		return nil, fmt.Errorf("invalid CT signature")
	}

	// Verify header
	hBytes, err := b64.DecodeString(hEnc)
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var hdr CTHeader
	if err := json.Unmarshal(hBytes, &hdr); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}
	if hdr.Alg != "Ed25519" || hdr.Typ != "ACP-CT-1.0" {
		return nil, fmt.Errorf("unsupported CT type: %s/%s", hdr.Typ, hdr.Alg)
	}

	// Parse payload
	pBytes, err := b64.DecodeString(pEnc)
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var payload CTPayload
	if err := json.Unmarshal(pBytes, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	// Check expiry
	if time.Now().Unix() > payload.ExpiresAt {
		return nil, fmt.Errorf("CT expired at %d", payload.ExpiresAt)
	}

	return &payload, nil
}

// CTHasCap returns true if the CT payload includes the requested capability.
func CTHasCap(payload *CTPayload, capability string) bool {
	for _, c := range payload.Capability {
		if c == capability {
			return true
		}
	}
	return false
}

// ParseBearerToken extracts the token from an "Authorization: Bearer <token>" header.
func ParseBearerToken(authHeader string) (string, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", fmt.Errorf("missing Bearer prefix in Authorization header")
	}
	tok := strings.TrimPrefix(authHeader, prefix)
	if tok == "" {
		return "", fmt.Errorf("empty token in Authorization header")
	}
	return tok, nil
}

// VerifyPoP verifies the Proof-of-Possession signature from a skill-agent.
// The agent signs: "METHOD|PATH|CHALLENGE|SHA256HEX(body)" with its Ed25519 key.
func VerifyPoP(method, path, challenge, bodyHashHex string, signature []byte, agentPubKey ed25519.PublicKey) error {
	sigInput := method + "|" + path + "|" + challenge + "|" + bodyHashHex
	if !ed25519.Verify(agentPubKey, []byte(sigInput), signature) {
		return fmt.Errorf("PoP signature verification failed")
	}
	return nil
}
