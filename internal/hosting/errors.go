package hosting

import (
	"errors"
	"time"
)

// ErrRejected means the host answered 401: the token is wrong or revoked.
var ErrRejected = errors.New("rejected the token")

// ErrNoAccount means no token is saved for the host.
var ErrNoAccount = errors.New("no account")

// NoAccessError means the account cannot see the repository (403 or 404).
type NoAccessError struct {
	Repo   string // owner/name
	Detail string
}

func (e *NoAccessError) Error() string {
	s := "No access to " + e.Repo + " with this account"
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	return s
}

// RateLimitError means the host's request limit is reached. Until is when to
// ask again when the host said so (Known); otherwise it is a guess.
type RateLimitError struct {
	Host  Host
	Until time.Time
	Known bool
}

func (e *RateLimitError) Error() string {
	if e.Host == GitHub && e.Known {
		return "GitHub: request limit reached, retrying at " + e.Until.Local().Format("15:04")
	}
	if e.Host == GitHub {
		return "GitHub: request limit reached, retrying shortly"
	}
	return "Bitbucket: request limit reached, retrying shortly"
}

// AccountError rewrites the "no access" answer to the account's own call
// (/user), which has no repository to name, into a message about the token.
// Any other error passes through.
func AccountError(h Host, err error) error {
	var na *NoAccessError
	if errors.As(err, &na) && na.Repo == "" {
		s := h.label() + " token can't read your account"
		if na.Detail != "" {
			s += ": " + na.Detail
		}
		return errors.New(s)
	}
	return err
}
