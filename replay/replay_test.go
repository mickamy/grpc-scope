package replay_test

import (
	"testing"

	"github.com/mickamy/grpc-scope/replay"
)

func TestParseMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantSvc string
		wantMtd string
		wantErr bool
	}{
		{
			name:    "valid method",
			input:   "/greeter.v1.GreeterService/SayHello",
			wantSvc: "greeter.v1.GreeterService",
			wantMtd: "SayHello",
		},
		{
			name:    "no leading slash",
			input:   "greeter.v1.GreeterService/SayHello",
			wantSvc: "greeter.v1.GreeterService",
			wantMtd: "SayHello",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only slash",
			input:   "/",
			wantErr: true,
		},
		{
			name:    "no method",
			input:   "/greeter.v1.GreeterService/",
			wantErr: true,
		},
		{
			name:    "no service",
			input:   "//SayHello",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc, mtd, err := replay.ParseMethod(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got svc=%q mtd=%q", tt.input, svc, mtd)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if svc != tt.wantSvc {
				t.Errorf("service: got %q, want %q", svc, tt.wantSvc)
			}
			if mtd != tt.wantMtd {
				t.Errorf("method: got %q, want %q", mtd, tt.wantMtd)
			}
		})
	}
}

func TestFilterMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string][]string
		wantKeys []string
		wantNil  bool
	}{
		{
			name:    "nil input",
			input:   nil,
			wantNil: true,
		},
		{
			name: "all filtered",
			input: map[string][]string{
				":authority":   {"example.com"},
				"content-type": {"application/grpc"},
				"grpc-timeout": {"30s"},
				"user-agent":   {"grpc-go/1.0"},
				"te":           {"trailers"},
			},
			wantNil: true,
		},
		{
			name: "some pass through",
			input: map[string][]string{
				"authorization": {"Bearer token"},
				"x-custom":      {"value"},
				"content-type":  {"application/grpc"},
				"grpc-timeout":  {"30s"},
			},
			wantKeys: []string{"authorization", "x-custom"},
		},
		{
			name: "case insensitive filtering",
			input: map[string][]string{
				"Content-Type": {"application/grpc"},
				"X-Custom":     {"value"},
			},
			wantKeys: []string{"x-custom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := replay.FilterMetadata(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.wantKeys) {
				t.Fatalf("expected %d keys, got %d: %v", len(tt.wantKeys), len(got), got)
			}
			for _, k := range tt.wantKeys {
				if _, ok := got[k]; !ok {
					t.Errorf("expected key %q in result, got %v", k, got)
				}
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	// NewClient should succeed even with an unreachable target (lazy connection).
	client, err := replay.NewClient("localhost:0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()
}

func TestRequest_EmptyPayload(t *testing.T) {
	t.Parallel()

	// Verify Request struct can hold empty payload.
	req := replay.Request{
		Method:      "/test.v1.TestService/Get",
		PayloadJSON: "",
	}

	if req.PayloadJSON != "" {
		t.Error("expected empty payload")
	}
}
