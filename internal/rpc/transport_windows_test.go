//go:build windows

package rpc

import (
	"testing"
)

func TestValidateEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		info    endpointInfo
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid 127.0.0.1 with tcp network",
			info:    endpointInfo{Network: "tcp", Address: "127.0.0.1:12345"},
			wantErr: false,
		},
		{
			name:    "valid 127.0.0.1 with empty network",
			info:    endpointInfo{Network: "", Address: "127.0.0.1:12345"},
			wantErr: false,
		},
		{
			name:    "valid localhost",
			info:    endpointInfo{Network: "tcp", Address: "localhost:12345"},
			wantErr: false,
		},
		{
			name:    "reject empty address",
			info:    endpointInfo{Network: "tcp", Address: ""},
			wantErr: true,
			errMsg:  "missing address",
		},
		{
			name:    "reject non-tcp network",
			info:    endpointInfo{Network: "udp", Address: "127.0.0.1:12345"},
			wantErr: true,
			errMsg:  "network must be tcp",
		},
		{
			name:    "reject remote address",
			info:    endpointInfo{Network: "tcp", Address: "10.0.0.1:12345"},
			wantErr: true,
			errMsg:  "address must bind to localhost",
		},
		{
			name:    "reject hostname",
			info:    endpointInfo{Network: "tcp", Address: "evil.example.com:12345"},
			wantErr: true,
			errMsg:  "address must bind to localhost",
		},
		{
			name:    "reject unix network",
			info:    endpointInfo{Network: "unix", Address: "/tmp/sock"},
			wantErr: true,
			errMsg:  "network must be tcp",
		},
		{
			name:    "reject 0.0.0.0 binding",
			info:    endpointInfo{Network: "tcp", Address: "0.0.0.0:12345"},
			wantErr: true,
			errMsg:  "address must bind to localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEndpoint(tt.info)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
