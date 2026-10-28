package loadgen

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// HashPasswords bcrypt-hashes each coalition password once at DefaultCost
// (never above the server cap of 12), so bots reuse the hash instead of paying
// bcrypt per join. The server compares the hash against the stored plaintext.
func HashPasswords(pw map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(pw))
	for name, p := range pw {
		h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("hash password for %q: %w", name, err)
		}
		out[name] = string(h)
	}
	return out, nil
}
