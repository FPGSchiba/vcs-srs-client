package session

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// serverMaxBcryptCost mirrors the server's utils.MaxBcryptCost. A hash above it
// is answered "Invalid password hash: bcrypt cost N exceeds the server maximum
// of 12", so the client must never hash above this cost.
const serverMaxBcryptCost = 12

func TestGuestHashCostWithinServerCap(t *testing.T) {
	if bcrypt.DefaultCost > serverMaxBcryptCost {
		t.Fatalf("bcrypt.DefaultCost = %d exceeds the server cap %d", bcrypt.DefaultCost, serverMaxBcryptCost)
	}

	raw, err := os.ReadFile("session.go")
	if err != nil {
		t.Fatalf("read session.go: %v", err)
	}
	src := strings.ReplaceAll(string(raw), "\r\n", "\n")
	const want = "bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)"
	if !strings.Contains(src, want) {
		t.Fatalf("session.go no longer hashes with %q; re-check the cost against the server cap of %d", want, serverMaxBcryptCost)
	}

	h, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cost, err := bcrypt.Cost(h)
	if err != nil {
		t.Fatalf("cost: %v", err)
	}
	if cost > serverMaxBcryptCost {
		t.Fatalf("generated hash cost %d exceeds server cap %d", cost, serverMaxBcryptCost)
	}
}
