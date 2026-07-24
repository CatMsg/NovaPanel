package core

import (
	"errors"
	"io"
	"net"
)

// shouldUntrackIOErr reports whether err indicates the connection is done.
func shouldUntrackIOErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return !netErr.Timeout()
	}
	return true
}
