// keygen generates an Ed25519 keypair for use as ACP institution keys.
// Output is printed as base64url-encoded strings suitable for env vars.
//
// Usage:
//
//	go run ./cmd/keygen
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
)

func main() {
	pub, prv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("keygen: %v", err)
	}

	pubB64 := base64.RawURLEncoding.EncodeToString(pub)
	// ed25519.PrivateKey is the 64-byte seed+public concatenation used by Go stdlib
	prvB64 := base64.RawURLEncoding.EncodeToString(prv)

	fmt.Printf("# Add these to your .env or docker-compose environment:\n\n")
	fmt.Printf("ACP_INSTITUTION_PUBLIC_KEY=%s\n", pubB64)
	fmt.Printf("ACP_INSTITUTION_PRIVATE_KEY=%s\n", prvB64)
}
