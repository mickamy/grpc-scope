package domain_test

import (
	"testing"
	"time"

	"github.com/mickamy/grpc-scope/scope/domain"
)

func TestCallEvent_IsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode domain.StatusCode
		want       bool
	}{
		{
			name:       "Unspecified is error",
			statusCode: domain.StatusUnspecified,
			want:       true,
		},
		{
			name:       "OK is not error",
			statusCode: domain.StatusOK,
			want:       false,
		},
		{
			name:       "Internal is error",
			statusCode: domain.StatusInternal,
			want:       true,
		},
		{
			name:       "NotFound is error",
			statusCode: domain.StatusNotFound,
			want:       true,
		},
		{
			name:       "Unauthenticated is error",
			statusCode: domain.StatusUnauthenticated,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := domain.CallEvent{
				ID:         "test-id",
				Method:     "/test.Service/Method",
				StartTime:  time.Now(),
				Duration:   10 * time.Millisecond,
				StatusCode: tt.statusCode,
			}

			if got := e.IsError(); got != tt.want {
				t.Errorf("IsError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusCode_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code domain.StatusCode
		want string
	}{
		{name: "Unspecified", code: domain.StatusUnspecified, want: "UNSPECIFIED"},
		{name: "OK", code: domain.StatusOK, want: "OK"},
		{name: "Cancelled", code: domain.StatusCancelled, want: "CANCELLED"},
		{name: "Unknown", code: domain.StatusUnknown, want: "UNKNOWN"},
		{name: "InvalidArgument", code: domain.StatusInvalidArgument, want: "INVALID_ARGUMENT"},
		{name: "DeadlineExceeded", code: domain.StatusDeadlineExceeded, want: "DEADLINE_EXCEEDED"},
		{name: "NotFound", code: domain.StatusNotFound, want: "NOT_FOUND"},
		{name: "AlreadyExists", code: domain.StatusAlreadyExists, want: "ALREADY_EXISTS"},
		{name: "PermissionDenied", code: domain.StatusPermissionDenied, want: "PERMISSION_DENIED"},
		{name: "ResourceExhausted", code: domain.StatusResourceExhausted, want: "RESOURCE_EXHAUSTED"},
		{name: "FailedPrecondition", code: domain.StatusFailedPrecondition, want: "FAILED_PRECONDITION"},
		{name: "Aborted", code: domain.StatusAborted, want: "ABORTED"},
		{name: "OutOfRange", code: domain.StatusOutOfRange, want: "OUT_OF_RANGE"},
		{name: "Unimplemented", code: domain.StatusUnimplemented, want: "UNIMPLEMENTED"},
		{name: "Internal", code: domain.StatusInternal, want: "INTERNAL"},
		{name: "Unavailable", code: domain.StatusUnavailable, want: "UNAVAILABLE"},
		{name: "DataLoss", code: domain.StatusDataLoss, want: "DATA_LOSS"},
		{name: "Unauthenticated", code: domain.StatusUnauthenticated, want: "UNAUTHENTICATED"},
		{name: "UnknownCode", code: domain.StatusCode(99), want: "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.code.String(); got != tt.want {
				t.Errorf("StatusCode.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
