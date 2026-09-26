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
// Empty strings are treated as absent at every level. Leading/trailing
// whitespace is trimmed from every string field before use.
//
// IPv6 literals must be bracketed ("[::1]") wherever a host and a port
// could otherwise appear in the same string — ConfigHost with no
// ConfigPort, and anywhere in ServerURL, Update, or Sync. A bare
// (unbracketed) IPv6 literal such as "::1:14447" is ambiguous (is that
// host "::1" port "14447", or a differently-split address?) and is
// rejected with an error rather than guessed at. ConfigHost alone (with
// ConfigPort supplied separately) may be a bare, unbracketed IPv6 literal,
// since there is no port in the same string to disambiguate against.
//
// Returns an error if no source yields a host, or if the resulting address
// is not a usable net.Dial target.
func Resolve(s Sources) (string, error) {
	s.Update = strings.TrimSpace(s.Update)
	s.Sync = strings.TrimSpace(s.Sync)
	s.ConfigHost = strings.TrimSpace(s.ConfigHost)
	s.ServerURL = strings.TrimSpace(s.ServerURL)

	// Precedence 1: Update
	if s.Update != "" {
		return validateDialAddress(s.Update)
	}

	// Precedence 2: Sync
	if s.Sync != "" {
		return validateDialAddress(s.Sync)
	}

	// Precedence 3: ConfigHost with ConfigPort (or DefaultVoicePort if ConfigPort is 0)
	if s.ConfigHost != "" {
		if _, _, err := net.SplitHostPort(s.ConfigHost); err == nil {
			return "", fmt.Errorf("voice config host %q already includes a port; move the port to the separate [voice] port key instead", s.ConfigHost)
		}
		port := s.ConfigPort
		if port == 0 {
			port = DefaultVoicePort
		}
		return validateDialAddress(net.JoinHostPort(s.ConfigHost, strconv.Itoa(port)))
	}

	// Precedence 4: ServerURL (strip scheme and port, use DefaultVoicePort)
	if s.ServerURL != "" {
		host, err := extractHostFromURL(s.ServerURL)
		if err != nil {
			return "", err
		}
		if host != "" {
			return validateDialAddress(net.JoinHostPort(host, strconv.Itoa(DefaultVoicePort)))
		}
	}

	// No source yielded a host
	return "", fmt.Errorf("no voice server address available")
}

// validateDialAddress confirms addr is usable as a net.Dial target: a
// non-empty host and a non-empty port, in "host:port" or "[ipv6]:port"
// form. It returns addr unchanged on success.
func validateDialAddress(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid voice server address %q: %w", addr, err)
	}
	if host == "" {
		return "", fmt.Errorf("invalid voice server address %q: missing host", addr)
	}
	if port == "" {
		return "", fmt.Errorf("invalid voice server address %q: missing port", addr)
	}
	return addr, nil
}

// extractHostFromURL parses a URL-like string and extracts just the
// hostname, stripping away scheme, path, and port.
func extractHostFromURL(urlStr string) (string, error) {
	// With an explicit scheme, delegate to url.Parse and trust its Host.
	// This path is unchanged from before: it already handles scheme,
	// port, and path correctly (e.g. "http://host:1234/api" -> "host").
	if strings.Contains(urlStr, "://") {
		u, err := url.Parse(urlStr)
		if err != nil {
			return "", fmt.Errorf("invalid server URL %q: %w", urlStr, err)
		}
		if u.Host == "" {
			return "", fmt.Errorf("server URL %q has no host", urlStr)
		}
		return stripPortOrHost(u.Host)
	}

	// No scheme: url.Parse would treat this as a relative reference (Host
	// empty, the whole string parked in Path), which is how a trailing
	// path segment used to leak into the returned host. Strip any path
	// ourselves before it can do that, then treat what's left as a bare
	// host or host:port.
	hostPort := urlStr
	if idx := strings.IndexByte(hostPort, '/'); idx >= 0 {
		hostPort = hostPort[:idx]
	}
	if hostPort == "" {
		return "", fmt.Errorf("server URL %q has no host", urlStr)
	}
	return stripPortOrHost(hostPort)
}

// stripPortOrHost removes a trailing :port from hostPort, if present,
// handling a bracketed IPv6 literal ([::1] or [::1]:port) correctly. A
// bare (unbracketed) IPv6 literal is rejected: it and a "host:port" string
// are both colon-separated and indistinguishable without brackets, which
// is exactly why the bracket convention exists.
func stripPortOrHost(hostPort string) (string, error) {
	if host, _, err := net.SplitHostPort(hostPort); err == nil {
		return host, nil
	}
	if strings.HasPrefix(hostPort, "[") && strings.HasSuffix(hostPort, "]") {
		// Bracketed IPv6 literal with no port, e.g. "[::1]".
		return hostPort[1 : len(hostPort)-1], nil
	}
	if strings.Contains(hostPort, ":") {
		return "", fmt.Errorf("ambiguous host %q: unbracketed IPv6 literals are not supported here, wrap it in brackets (e.g. [%s])", hostPort, hostPort)
	}
	// Plain host, no port.
	return hostPort, nil
}
