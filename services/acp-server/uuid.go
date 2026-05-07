package main

import (
	"crypto/rand"
	"fmt"
)

// newUUID generates a random UUID v4 string using crypto/rand.
func newUUID() string {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic("acp-server: crypto/rand unavailable: " + err.Error())
	}
	// Set version 4 and variant bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
