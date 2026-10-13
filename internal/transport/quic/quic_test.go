package quictransport

import (
	"errors"
	"testing"

	"github.com/quic-go/quic-go"
)

func TestIsGracefulCloseRequiresRemoteZeroCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "remote zero code", err: &quic.ApplicationError{Remote: true, ErrorCode: 0}, want: true},
		{name: "remote application failure", err: &quic.ApplicationError{Remote: true, ErrorCode: 1}},
		{name: "local close", err: &quic.ApplicationError{Remote: false, ErrorCode: 0}},
		{name: "other error", err: errors.New("read failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsGracefulClose(tc.err); got != tc.want {
				t.Fatalf("IsGracefulClose(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}
