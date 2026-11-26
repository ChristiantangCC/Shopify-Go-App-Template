// Package retry marks errors a background job should not retry. Webhook and
// Events handlers return Permanent(err) for failures that resending can
// never fix (a payload that won't decode, a resource that no longer exists);
// the job workers turn those into a cancelled job instead of a retry.
package retry

import "errors"

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent wraps err so the job running it is cancelled, not retried.
// A nil err stays nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err, or an error it wraps, came from Permanent.
func IsPermanent(err error) bool {
	var permanent permanentError
	return errors.As(err, &permanent)
}
