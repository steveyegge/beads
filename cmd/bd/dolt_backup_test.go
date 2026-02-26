package main

import (
	"testing"
	"time"
)

func TestGetDoltBackupInterval(t *testing.T) {
	old := doltBackupInterval
	defer func() { doltBackupInterval = old }()

	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr bool
	}{
		{"off", "off", 0, false},
		{"empty", "", 0, false},
		{"zero", "0", 0, false},
		{"15m", "15m", 15 * time.Minute, false},
		{"1h", "1h", time.Hour, false},
		{"30s", "30s", 30 * time.Second, false},
		{"OFF uppercase", "OFF", 0, false},
		{"with spaces", "  15m  ", 15 * time.Minute, false},
		{"invalid", "bogus", 0, true},
		{"negative", "-5m", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doltBackupInterval = tt.value
			got, err := getDoltBackupInterval()
			if (err != nil) != tt.wantErr {
				t.Fatalf("getDoltBackupInterval() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("getDoltBackupInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}
