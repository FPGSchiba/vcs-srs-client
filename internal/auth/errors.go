package auth

import "errors"

// ErrGuestUnavailable means the server reports no guest login support.
var ErrGuestUnavailable = errors.New("guest login not available on this server")

// ErrLoginRejected means the server rejected the guest credentials.
var ErrLoginRejected = errors.New("login rejected")
