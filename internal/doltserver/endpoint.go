package doltserver

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
)

type EndpointPurpose string

const (
	EndpointPurposeRead      EndpointPurpose = "read"
	EndpointPurposeLifecycle EndpointPurpose = "lifecycle"
	EndpointPurposeConfig    EndpointPurpose = "config"
	EndpointPurposeDoctorFix EndpointPurpose = "doctor-fix"
)

type EndpointOwner string

const (
	EndpointOwnerBeads    EndpointOwner = "beads"
	EndpointOwnerShared   EndpointOwner = "shared"
	EndpointOwnerExternal EndpointOwner = "external"
	EndpointOwnerGC       EndpointOwner = "gc"
	EndpointOwnerEmbedded EndpointOwner = "embedded"
	EndpointOwnerUnknown  EndpointOwner = "unknown"
)

type EndpointSource string

const (
	EndpointSourceEnv           EndpointSource = "env"
	EndpointSourcePortFile      EndpointSource = "port-file"
	EndpointSourceConfigYAML    EndpointSource = "config-yaml"
	EndpointSourceMetadata      EndpointSource = "metadata"
	EndpointSourceSharedDefault EndpointSource = "shared-default"
	EndpointSourceUnresolved    EndpointSource = "unresolved"
)

type Diagnostic struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type Endpoint struct {
	Host     string
	Port     int
	Database string
	User     string
	TLS      bool

	Owner  EndpointOwner
	Source EndpointSource

	Resolved bool
	Dialable bool
}

type ResolveOptions struct {
	Purpose EndpointPurpose
}

type PortWriteReason string

const (
	PortWriteReasonStartedServer PortWriteReason = "started-server"
	PortWriteReasonSetPort       PortWriteReason = "set-port"
	PortWriteReasonDoctorHandoff PortWriteReason = "doctor-handoff"
)

func ResolveEndpoint(beadsDir string, opts ResolveOptions) (Endpoint, []Diagnostic, error) {
	if opts.Purpose == "" {
		opts.Purpose = EndpointPurposeRead
	}

	var diagnostics []Diagnostic
	cfg := loadEndpointConfig(beadsDir, &diagnostics)
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	host := cfg.GetDoltServerHost()
	owner := resolveEndpointOwner(cfg)
	if owner == EndpointOwnerEmbedded {
		return Endpoint{
			Host:     host,
			Database: cfg.GetDoltDatabase(),
			User:     cfg.GetDoltServerUser(),
			TLS:      cfg.GetDoltServerTLS(),
			Owner:    EndpointOwnerEmbedded,
			Source:   EndpointSourceUnresolved,
		}, diagnostics, nil
	}

	port, source := resolveEndpointPort(beadsDir, owner, cfg)
	if port <= 0 {
		diagnostics = append(diagnostics, Diagnostic{
			Level:   "error",
			Message: "Dolt server endpoint is unresolved; port 0 is not dialable",
		})
		return Endpoint{
			Host:     host,
			Port:     0,
			Database: cfg.GetDoltDatabase(),
			User:     cfg.GetDoltServerUser(),
			TLS:      cfg.GetDoltServerTLS(),
			Owner:    owner,
			Source:   EndpointSourceUnresolved,
		}, diagnostics, nil
	}

	return Endpoint{
		Host:     host,
		Port:     port,
		Database: cfg.GetDoltDatabase(),
		User:     cfg.GetDoltServerUser(),
		TLS:      cfg.GetDoltServerTLS(),
		Owner:    owner,
		Source:   source,
		Resolved: true,
		Dialable: true,
	}, diagnostics, nil
}

func WriteResolvedPort(beadsDir string, port int, reason PortWriteReason) error {
	if port <= 0 {
		return fmt.Errorf("refusing to write unresolved Dolt port %d", port)
	}
	switch reason {
	case PortWriteReasonStartedServer, PortWriteReasonSetPort, PortWriteReasonDoctorHandoff:
	default:
		return fmt.Errorf("refusing to write Dolt port without explicit reason: %q", reason)
	}
	if existing := readPortFile(beadsDir); existing > 0 && existing != port && reason == PortWriteReasonStartedServer {
		return fmt.Errorf("refusing to overwrite existing Dolt port file %d with started server port %d; use an explicit handoff or bd dolt set port", existing, port)
	}
	return writePortFile(beadsDir, port)
}

func loadEndpointConfig(beadsDir string, diagnostics *[]Diagnostic) *configfile.Config {
	if beadsDir == "" {
		return nil
	}
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		*diagnostics = append(*diagnostics, Diagnostic{
			Level:   "warning",
			Message: fmt.Sprintf("failed to load metadata.json: %v", err),
		})
		return nil
	}
	return cfg
}

func resolveEndpointOwner(cfg *configfile.Config) EndpointOwner {
	if cfg != nil && strings.ToLower(cfg.DoltMode) == configfile.DoltModeEmbedded && cfg.DoltMode != "" &&
		os.Getenv("BEADS_DOLT_SERVER_MODE") != "1" && !IsSharedServerMode() {
		return EndpointOwnerEmbedded
	}
	if os.Getenv("GT_ROOT") != "" && os.Getenv("BEADS_DOLT_PORT") != "" {
		return EndpointOwnerGC
	}
	if IsSharedServerMode() {
		return EndpointOwnerShared
	}
	if IsAutoStartDisabled() || os.Getenv("BEADS_DOLT_SERVER_MODE") == "1" || (cfg != nil && cfg.DoltServerPort > 0) {
		return EndpointOwnerExternal
	}
	return EndpointOwnerBeads
}

func resolveEndpointPort(beadsDir string, owner EndpointOwner, cfg *configfile.Config) (int, EndpointSource) {
	if p := os.Getenv("BEADS_DOLT_SERVER_PORT"); p != "" {
		if port, err := strconv.Atoi(p); err == nil && port > 0 {
			return port, EndpointSourceEnv
		}
	}
	if p := os.Getenv("BEADS_DOLT_PORT"); p != "" {
		if port, err := strconv.Atoi(p); err == nil && port > 0 {
			return port, EndpointSourceEnv
		}
	}

	portDir := beadsDir
	if owner == EndpointOwnerShared {
		if sharedDir, err := SharedServerDir(); err == nil {
			portDir = sharedDir
		}
	}
	if p := readPortFile(portDir); p > 0 {
		return p, EndpointSourcePortFile
	}

	if p := config.GetYamlConfig("dolt.port"); p != "" {
		if port, err := strconv.Atoi(p); err == nil && port > 0 {
			return port, EndpointSourceConfigYAML
		}
	}
	if p := config.GetStringFromDir(beadsDir, "dolt.port"); p != "" {
		if port, err := strconv.Atoi(p); err == nil && port > 0 {
			return port, EndpointSourceConfigYAML
		}
	}
	if cfg != nil && cfg.DoltServerPort > 0 {
		return cfg.DoltServerPort, EndpointSourceMetadata
	}
	if owner == EndpointOwnerShared {
		return DefaultSharedServerPort, EndpointSourceSharedDefault
	}
	return 0, EndpointSourceUnresolved
}
