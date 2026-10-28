package loadgen

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// serverMaxBcryptCost mirrors the server's cap (srs/auth_service.go:487-493);
// see also internal/session/bcrypt_cost_test.go:11-28 in this repo. A hash
// above it is rejected by the server.
const serverMaxBcryptCost = 12

func TestHashPasswordsDefaultCost(t *testing.T) {
	if bcrypt.DefaultCost > serverMaxBcryptCost {
		t.Fatalf("bcrypt.DefaultCost = %d exceeds server cap %d", bcrypt.DefaultCost, serverMaxBcryptCost)
	}
	pw := map[string]string{"Blue": "x", "Red": "y"}
	got, err := HashPasswords(pw)
	if err != nil {
		t.Fatalf("HashPasswords: %v", err)
	}
	if len(got) != len(pw) {
		t.Fatalf("got %d hashes, want %d", len(got), len(pw))
	}
	for name, p := range pw {
		h, ok := got[name]
		if !ok {
			t.Fatalf("missing hash for %s", name)
		}
		cost, err := bcrypt.Cost([]byte(h))
		if err != nil || cost != bcrypt.DefaultCost {
			t.Fatalf("%s: cost %d err %v, want %d", name, cost, err, bcrypt.DefaultCost)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(h), []byte(p)); err != nil {
			t.Fatalf("%s: compare: %v", name, err)
		}
	}
}
