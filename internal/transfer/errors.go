package transfer

import (
	"context"
	"errors"
	"fmt"

	quictransport "dropmesh/internal/transport/quic"
)

type FailureKind string

const (
	FailureAuthentication FailureKind = "authentication"
	FailureProtocol       FailureKind = "protocol"
	FailureTimeout        FailureKind = "timeout"
	FailureCancellation   FailureKind = "cancellation"
	FailureIntegrity      FailureKind = "integrity"
	FailureFilesystem     FailureKind = "filesystem"
	FailureResourceLimit  FailureKind = "resource-limit"
	FailureTransport      FailureKind = "transport"
)

type Failure struct {
	Kind  FailureKind
	cause error
}

func (e *Failure) Error() string { return fmt.Sprintf("%s failure: %v", e.Kind, e.cause) }
func (e *Failure) Unwrap() error { return e.cause }

func fail(kind FailureKind, cause error) error {
	if cause == nil {
		return nil
	}
	return &Failure{Kind: kind, cause: cause}
}

func classifyContext(err error, fallback FailureKind) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fail(FailureCancellation, err)
	case errors.Is(err, context.DeadlineExceeded):
		return fail(FailureTimeout, err)
	case quictransport.IsTimeout(err):
		return fail(FailureTimeout, err)
	default:
		return fail(fallback, err)
	}
}

func classifyOperation(ctx context.Context, err error, fallback FailureKind) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return classifyContext(ctxErr, fallback)
	}
	return classifyContext(err, fallback)
}

func FailureCategory(err error) FailureKind {
	var classified *Failure
	if errors.As(err, &classified) {
		return classified.Kind
	}
	return ""
}
