package voice

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Sources carries every place a voice address can come from, in no
// particular order; Resolve applies the precedence.
type Sources struct {
	Update     string // VoiceAddressUpdate.coalition_voice_addr (live)
	Sync       string // ServerSyncResult.coalition_voice_addr
	ConfigHost string // [voice] host
	ConfigPort int    // [voice] port
	ServerURL  string // [server_url], for host derivation
}

const DefaultVoicePort = 5002

// Resolve returns a dialable host:port.
// Precedence: Update > Sync > ConfigHost/ConfigPort > ServerURL.
// Empty strings are treated as absent at every level.
// Returns an error if no source yields a host.
func Resolve(s Sources) (string, error) {
	// Precedence 1: Update
	if s.Update != "" {
		return s.Update, nil
	}

	// Precedence 2: Sync
	if s.Sync != "" {
		return s.Sync, nil
	}

	// Precedence 3: ConfigHost with ConfigPort (or DefaultVoicePort if ConfigPort is 0)
	if s.ConfigHost != "" {
		port := s.ConfigPort
		if port == 0 {
			port = DefaultVoicePort
		}
		return net.JoinHostPort(s.ConfigHost, strconv.Itoa(port)), nil
	}

	// Precedence 4: ServerURL (strip scheme and port, use DefaultVoicePort)
	if s.ServerURL != "" {
		host, err := extractHostFromURL(s.ServerURL)
		if err != nil {
			return "", err
		}
		if host != "" {
			return net.JoinHostPort(host, strconv.Itoa(DefaultVoicePort)), nil
		}
	}

	// No source yielded a host
	return "", fmt.Errorf("no voice server address available")
}

// extractHostFromURL parses a URL-like string and extracts just the hostname,
// stripping away scheme and port.
func extractHostFromURL(urlStr string) (string, error) {
	// Try to parse as a full URL first
	u, err := url.Parse(urlStr)
	if err != nil || u.Host == "" {
		// If that fails, try to parse it as a host:port directly
		// This handles cases like "vcs.example.com:14447" or "vcs.example.com"
		host, _, err := net.SplitHostPort(urlStr)
		if err != nil {
			// SplitHostPort fails if there's no port, but that's OK
			// If the whole string is just a host (no colon), we can use it directly
			if strings.Contains(urlStr, ":") && !strings.HasPrefix(urlStr, "[") {
				// Has a colon but couldn't parse (not IPv6), error
				return "", err
			}
			// Either no colon or IPv6 literal without port
			return urlStr, nil
		}
		return host, nil
	}

	// Successfully parsed as URL, return the Host (which may include port)
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		// No port in u.Host, use it directly
		return u.Host, nil
	}
	return host, nil
}
