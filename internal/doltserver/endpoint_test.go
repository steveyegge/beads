package doltserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

func writeEndpointMetadata(t *testing.T, beadsDir string) {
	t.Helper()
	cfg := configfile.DefaultConfig()
	cfg.DoltMode = configfile.DoltModeServer
	cfg.DoltDatabase = "beads_test"
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
}

func isolateEndpointEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"BEADS_DOLT_SERVER_PORT",
		"BEADS_DOLT_PORT",
		"BEADS_DOLT_SHARED_SERVER",
		"BEADS_DOLT_SERVER_MODE",
		"BEADS_DOLT_AUTO_START",
		"GT_ROOT",
	} {
		t.Setenv(key, "")
	}
}

func TestResolveEndpointReadUsesPortFileWithoutMutating(t *testing.T) {
	isolateEndpointEnv(t)
	beadsDir := t.TempDir()
	writeEndpointMetadata(t, beadsDir)
	portPath := filepath.Join(beadsDir, PortFileName)
	if err := os.WriteFile(portPath, []byte("14567"), 0o600); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	endpoint, diagnostics, err := ResolveEndpoint(beadsDir, ResolveOptions{Purpose: EndpointPurposeRead})
	if err != nil {
		t.Fatalf("ResolveEndpoint: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
	if !endpoint.Resolved || !endpoint.Dialable {
		t.Fatalf("endpoint should be resolved and dialable: %#v", endpoint)
	}
	if endpoint.Port != 14567 || endpoint.Source != EndpointSourcePortFile {
		t.Fatalf("endpoint = %#v, want port-file port 14567", endpoint)
	}
	data, err := os.ReadFile(portPath)
	if err != nil {
		t.Fatalf("read port file: %v", err)
	}
	if string(data) != "14567" {
		t.Fatalf("port file mutated to %q", string(data))
	}
}

func TestResolveEndpointUnresolvedPortIsNotDialable(t *testing.T) {
	isolateEndpointEnv(t)
	beadsDir := t.TempDir()
	writeEndpointMetadata(t, beadsDir)

	endpoint, diagnostics, err := ResolveEndpoint(beadsDir, ResolveOptions{Purpose: EndpointPurposeRead})
	if err != nil {
		t.Fatalf("ResolveEndpoint: %v", err)
	}
	if endpoint.Resolved || endpoint.Dialable || endpoint.Port != 0 {
		t.Fatalf("endpoint = %#v, want unresolved port 0", endpoint)
	}
	if endpoint.Source != EndpointSourceUnresolved {
		t.Fatalf("source = %q, want unresolved", endpoint.Source)
	}
	if len(diagnostics) == 0 {
		t.Fatal("expected unresolved diagnostic")
	}
}

func TestWriteResolvedPortRequiresExplicitReasonAndPositivePort(t *testing.T) {
	isolateEndpointEnv(t)
	beadsDir := t.TempDir()

	if err := WriteResolvedPort(beadsDir, 0, PortWriteReasonStartedServer); err == nil {
		t.Fatal("WriteResolvedPort accepted port 0")
	}
	if err := WriteResolvedPort(beadsDir, 12345, ""); err == nil {
		t.Fatal("WriteResolvedPort accepted empty reason")
	}
	if err := WriteResolvedPort(beadsDir, 12345, PortWriteReasonStartedServer); err != nil {
		t.Fatalf("WriteResolvedPort: %v", err)
	}
	if got := readPortFile(beadsDir); got != 12345 {
		t.Fatalf("readPortFile = %d, want 12345", got)
	}
}

func TestWriteResolvedPortRefusesImplicitStartedServerSwitch(t *testing.T) {
	isolateEndpointEnv(t)
	beadsDir := t.TempDir()
	if err := WriteResolvedPort(beadsDir, 12345, PortWriteReasonSetPort); err != nil {
		t.Fatalf("initial WriteResolvedPort: %v", err)
	}
	if err := WriteResolvedPort(beadsDir, 12346, PortWriteReasonStartedServer); err == nil {
		t.Fatal("WriteResolvedPort allowed started-server overwrite of existing port")
	}
	if got := readPortFile(beadsDir); got != 12345 {
		t.Fatalf("readPortFile = %d, want original 12345", got)
	}
	if err := WriteResolvedPort(beadsDir, 12346, PortWriteReasonDoctorHandoff); err != nil {
		t.Fatalf("doctor handoff WriteResolvedPort: %v", err)
	}
	if got := readPortFile(beadsDir); got != 12346 {
		t.Fatalf("readPortFile = %d, want handoff port 12346", got)
	}
}
