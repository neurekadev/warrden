package arr

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// AuthFailure reports whether an error represents a rejected API key.
func AuthFailure(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && (httpErr.StatusCode == 401 || httpErr.StatusCode == 403)
}

// EnvironmentalFailure reports whether an error came from an arr or its network.
func EnvironmentalFailure(err error) bool {
	var httpErr *HTTPError
	var netErr net.Error
	return errors.As(err, &httpErr) || errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// Detail formats an error using the legacy external type labels where required.
func Detail(err error) string {
	type legacy interface{ LegacyError() string }
	var value legacy
	if errors.As(err, &value) {
		return value.LegacyError()
	}
	return fmt.Sprintf("%T: %s", err, err)
}
