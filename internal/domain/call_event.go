package domain

import "time"

// StatusCode represents a gRPC status code.
type StatusCode int32

const (
	StatusUnspecified        StatusCode = iota // zero value = unset
	StatusOK                                   // gRPC 0
	StatusCancelled                            // gRPC 1
	StatusUnknown                              // gRPC 2
	StatusInvalidArgument                      // gRPC 3
	StatusDeadlineExceeded                     // gRPC 4
	StatusNotFound                             // gRPC 5
	StatusAlreadyExists                        // gRPC 6
	StatusPermissionDenied                     // gRPC 7
	StatusResourceExhausted                    // gRPC 8
	StatusFailedPrecondition                   // gRPC 9
	StatusAborted                              // gRPC 10
	StatusOutOfRange                           // gRPC 11
	StatusUnimplemented                        // gRPC 12
	StatusInternal                             // gRPC 13
	StatusUnavailable                          // gRPC 14
	StatusDataLoss                             // gRPC 15
	StatusUnauthenticated                      // gRPC 16
)

// Metadata represents gRPC metadata (headers/trailers).
type Metadata map[string][]string

// CallEvent represents a single captured gRPC call.
type CallEvent struct {
	ID               string
	Method           string
	StartTime        time.Time
	Duration         time.Duration
	StatusCode       StatusCode
	StatusMessage    string
	RequestMetadata  Metadata
	ResponseHeaders  Metadata
	ResponseTrailers Metadata
	RequestPayload   string
	ResponsePayload  string
}

// IsError reports whether the call ended with a non-OK status.
func (e CallEvent) IsError() bool {
	return e.StatusCode != StatusOK
}

// StatusCodeString returns the short string representation of the status code.
func (c StatusCode) String() string {
	switch c {
	case StatusUnspecified:
		return "UNSPECIFIED"
	case StatusOK:
		return "OK"
	case StatusCancelled:
		return "CANCELLED"
	case StatusUnknown:
		return "UNKNOWN"
	case StatusInvalidArgument:
		return "INVALID_ARGUMENT"
	case StatusDeadlineExceeded:
		return "DEADLINE_EXCEEDED"
	case StatusNotFound:
		return "NOT_FOUND"
	case StatusAlreadyExists:
		return "ALREADY_EXISTS"
	case StatusPermissionDenied:
		return "PERMISSION_DENIED"
	case StatusResourceExhausted:
		return "RESOURCE_EXHAUSTED"
	case StatusFailedPrecondition:
		return "FAILED_PRECONDITION"
	case StatusAborted:
		return "ABORTED"
	case StatusOutOfRange:
		return "OUT_OF_RANGE"
	case StatusUnimplemented:
		return "UNIMPLEMENTED"
	case StatusInternal:
		return "INTERNAL"
	case StatusUnavailable:
		return "UNAVAILABLE"
	case StatusDataLoss:
		return "DATA_LOSS"
	case StatusUnauthenticated:
		return "UNAUTHENTICATED"
	default:
		return "UNKNOWN"
	}
}
