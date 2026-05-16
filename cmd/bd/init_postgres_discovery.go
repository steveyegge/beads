package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// discoverLocalPostgres reads a Postgres cluster directory to determine the
// host and port of a running PG instance.
//
// Discovery order:
//  1. postmaster.pid — line 4 is the port, line 6 is the socket/listen host.
//     If the file is truncated (<6 lines), warns and falls through to step 2.
//  2. postgresql.conf — parse the port and listen_addresses directives.
//     Returns found=false when both sources are absent or unparseable.
//
// discoverLocalPostgres never modifies disk state (NFR-2) and is designed to
// complete in under 50ms on the happy path (NFR-1).
func discoverLocalPostgres(clusterDir string) (host string, port int, found bool) {
	if _, err := os.Stat(clusterDir); err != nil {
		return "", 0, false
	}

	pidHost, pidPort, pidOK := readPostmasterPid(clusterDir)
	if pidOK {
		return pidHost, pidPort, true
	}

	confHost, confPort, confOK := readPostgresConf(clusterDir)
	if confOK {
		return confHost, confPort, true
	}

	return "", 0, false
}

// readPostmasterPid attempts to extract host and port from postmaster.pid.
// Returns ok=false and warns if the file is missing or truncated.
func readPostmasterPid(clusterDir string) (host string, port int, ok bool) {
	pidPath := filepath.Join(clusterDir, "postmaster.pid")
	f, err := os.Open(pidPath) // #nosec G304 -- path is constrained to a user-controlled cluster directory
	if err != nil {
		return "", 0, false
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, strings.TrimSpace(scanner.Text()))
	}

	// postmaster.pid layout (0-indexed):
	//   line 0: PID
	//   line 1: data directory
	//   line 2: start time
	//   line 3: port
	//   line 4: socket directory
	//   line 5: listen host (first listen_address)
	if len(lines) < 6 {
		fmt.Fprintf(os.Stderr, "bd: postmaster.pid has only %d line(s), expected ≥6; falling back to postgresql.conf\n", len(lines)) // #nosec G705 -- stderr only, no browser context
		return "", 0, false
	}

	p, err := strconv.Atoi(lines[3])
	if err != nil || p <= 0 || p > 65535 {
		fmt.Fprintf(os.Stderr, "bd: postmaster.pid line 4 port %q is invalid; falling back to postgresql.conf\n", lines[3]) // #nosec G705 -- stderr only, no browser context
		return "", 0, false
	}

	h := lines[5]
	if h == "" || h == "*" {
		h = "127.0.0.1"
	}
	// Strip UNIX-socket prefix if present (e.g. "/tmp")
	if strings.HasPrefix(h, "/") {
		h = "127.0.0.1"
	}

	return h, p, true
}

// readPostgresConf parses port and listen_addresses from postgresql.conf.
func readPostgresConf(clusterDir string) (host string, port int, ok bool) {
	confPath := filepath.Join(clusterDir, "postgresql.conf")
	f, err := os.Open(confPath) // #nosec G304 -- path is constrained to a user-controlled cluster directory
	if err != nil {
		return "", 0, false
	}
	defer f.Close()

	var foundPort int
	var foundHost string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "port") {
			val := parseConfValue(line, "port")
			if p, err := strconv.Atoi(val); err == nil && p > 0 && p <= 65535 {
				foundPort = p
			}
		}

		if strings.HasPrefix(line, "listen_addresses") {
			val := parseConfValue(line, "listen_addresses")
			// Strip surrounding quotes.
			val = strings.Trim(val, "'\"")
			if val != "" && val != "*" {
				// Take the first address in a comma-separated list.
				if idx := strings.IndexByte(val, ','); idx >= 0 {
					val = strings.TrimSpace(val[:idx])
				}
				foundHost = val
			}
		}
	}

	if foundPort == 0 {
		return "", 0, false
	}
	if foundHost == "" || foundHost == "*" {
		foundHost = "127.0.0.1"
	}
	return foundHost, foundPort, true
}

// parseConfValue extracts the value from a postgresql.conf directive line of
// the form:   key = value   or   key = 'value'   (with optional comment).
func parseConfValue(line, key string) string {
	// Remove inline comment.
	if idx := strings.IndexByte(line, '#'); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	// Expect "key = value" or "key=value".
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return ""
	}
	k := strings.TrimSpace(line[:eq])
	if k != key {
		return ""
	}
	return strings.TrimSpace(line[eq+1:])
}
