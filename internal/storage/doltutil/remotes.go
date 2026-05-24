package doltutil

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/remotecache"
	"github.com/steveyegge/beads/internal/storage"
)

// ShellQuote returns s wrapped in single quotes with any embedded single
// quotes escaped, making it safe to interpolate into a shell command string.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// IsSSHURL returns true if the URL uses SSH transport.
// Matches git+ssh://, ssh://, and git@host: patterns.
func IsSSHURL(url string) bool {
	return strings.HasPrefix(url, "git+ssh://") ||
		strings.HasPrefix(url, "ssh://") ||
		strings.HasPrefix(url, "git@")
}

// IsGitProtocolURL returns true if the URL uses the git wire protocol.
// This includes SSH transports (git+ssh://, ssh://, git@host:) and
// git-over-HTTPS (git+https://) and plain git:// protocol.
// These remotes involve network I/O that can exceed MySQL connection
// timeouts and should use CLI-based push/pull instead of SQL.
func IsGitProtocolURL(url string) bool {
	return IsSSHURL(url) ||
		strings.HasPrefix(url, "git+https://") ||
		strings.HasPrefix(url, "git+http://") ||
		strings.HasPrefix(url, "git://")
}

// --- per-write `dolt remote -v` fork mitigation (gastown#4070 / beads#3948) ---
// `dolt remote -v` shells out to the dolt CLI, which loads the whole DB into
// memory (~2GB on large stores) even just to print the remote list. In a
// multi-store federation, the write path resolves peers via ListCLIRemotes /
// FindCLIRemote on every write, forking one ~2GB process per peer-check and
// OOM-storming the box. Remotes change rarely (manual `bd dolt remote
// add/remove`), so we cache the parsed list per dbPath with a short TTL and
// invalidate on mutation. Set BEADS_NO_REMOTE_CACHE=1 to disable (fall back to
// the original uncached behavior).

// remoteListTTL bounds how long a parsed remote list is cached per dbPath.
// Remotes are stable infrastructure (set at rig creation) and bd-driven
// mutations invalidate the cache, so a long default is safe and minimizes the
// 2GB `dolt remote -v` forks. Override with BEADS_REMOTE_CACHE_TTL_SEC.
var remoteListTTL = func() time.Duration {
	if s := strings.TrimSpace(os.Getenv("BEADS_REMOTE_CACHE_TTL_SEC")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 30 * time.Minute
}()

func remoteCacheDisabled() bool {
	v := os.Getenv("BEADS_NO_REMOTE_CACHE")
	return v == "1" || strings.EqualFold(v, "true")
}

func remoteCacheFile(dbPath string) string {
	h := sha256.Sum256([]byte(dbPath))
	return filepath.Join(os.TempDir(), "bd-remotes-"+hex.EncodeToString(h[:8])+".json")
}

type remoteCacheEntry struct {
	Stamp   time.Time            `json:"stamp"`
	Remotes []storage.RemoteInfo `json:"remotes"`
}

func readRemoteCache(dbPath string) ([]storage.RemoteInfo, bool) {
	if remoteCacheDisabled() {
		return nil, false
	}
	b, err := os.ReadFile(remoteCacheFile(dbPath))
	if err != nil {
		return nil, false
	}
	var e remoteCacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, false
	}
	// Reject expired or clock-skewed (future-stamped) entries.
	if time.Since(e.Stamp) > remoteListTTL || e.Stamp.After(time.Now()) {
		return nil, false
	}
	return e.Remotes, true
}

func writeRemoteCache(dbPath string, remotes []storage.RemoteInfo) {
	if remoteCacheDisabled() {
		return
	}
	b, err := json.Marshal(remoteCacheEntry{Stamp: time.Now(), Remotes: remotes})
	if err != nil {
		return
	}
	f := remoteCacheFile(dbPath)
	tmp := fmt.Sprintf("%s.%d.tmp", f, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, f); err != nil {
		_ = os.Remove(tmp)
	}
}

// InvalidateCLIRemotes drops the cached remote list for dbPath. Called after
// any CLI-level remote mutation so the next read re-forks dolt exactly once.
func InvalidateCLIRemotes(dbPath string) {
	_ = os.Remove(remoteCacheFile(dbPath))
}

// ListCLIRemotes parses `dolt remote -v` output from the given database
// directory. The result is cached per dbPath (see the mitigation note above);
// AddCLIRemote/RemoveCLIRemote invalidate it.
func ListCLIRemotes(dbPath string) ([]storage.RemoteInfo, error) {
	if cached, ok := readRemoteCache(dbPath); ok {
		return cached, nil
	}
	// Skip the fork entirely when dbPath isn't a dolt repository: `dolt remote -v`
	// loads the whole DB (~2GB) only to error "not a valid dolt repository". The
	// multi-store write path calls this with non-dolt dirs (e.g. the agent CWD),
	// which is the per-write fork storm (gastown#4070 / beads#3948).
	if fi, statErr := os.Stat(filepath.Join(dbPath, ".dolt")); statErr != nil || !fi.IsDir() {
		writeRemoteCache(dbPath, nil)
		return nil, nil
	}
	cmd := exec.Command("dolt", "remote", "-v") // #nosec G204 -- fixed command
	cmd.Dir = dbPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeRemoteCache(dbPath, nil) // negative-cache transient errors to avoid re-forking
		return nil, fmt.Errorf("dolt remote -v failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	seen := map[string]bool{}
	var remotes []storage.RemoteInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// dolt remote -v outputs: name <whitespace> url [<whitespace> (fetch|push)]
		parts := strings.Fields(line)
		if len(parts) >= 2 && !seen[parts[0]] {
			seen[parts[0]] = true
			remotes = append(remotes, storage.RemoteInfo{Name: parts[0], URL: parts[1]})
		}
	}
	writeRemoteCache(dbPath, remotes)
	return remotes, nil
}

// AddCLIRemote adds a remote at the filesystem level via dolt CLI.
// Both name and URL are validated before being passed to exec.Command
// as a defense-in-depth measure.
func AddCLIRemote(dbPath, name, url string) error {
	if err := remotecache.ValidateRemoteName(name); err != nil {
		return fmt.Errorf("invalid remote name: %w", err)
	}
	if err := remotecache.ValidateRemoteURL(url); err != nil {
		return fmt.Errorf("invalid remote URL: %w", err)
	}
	cmd := exec.Command("dolt", "remote", "add", name, url) // #nosec G204
	cmd.Dir = dbPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("dolt remote add failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	InvalidateCLIRemotes(dbPath)
	return nil
}

// RemoveCLIRemote removes a remote at the filesystem level via dolt CLI.
// The name is validated before being passed to exec.Command.
func RemoveCLIRemote(dbPath, name string) error {
	if err := remotecache.ValidateRemoteName(name); err != nil {
		return fmt.Errorf("invalid remote name: %w", err)
	}
	cmd := exec.Command("dolt", "remote", "remove", name) // #nosec G204
	cmd.Dir = dbPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("dolt remote remove failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	InvalidateCLIRemotes(dbPath)
	return nil
}

// FindCLIRemote returns the URL for a named CLI remote, or "" if not found.
func FindCLIRemote(dbPath, name string) string {
	remotes, err := ListCLIRemotes(dbPath)
	if err != nil {
		return ""
	}
	for _, r := range remotes {
		if r.Name == name {
			return r.URL
		}
	}
	return ""
}

// ToRemoteNameMap converts a RemoteInfo slice to a map keyed by name.
// Useful for de-duplicating remotes (e.g., from `dolt remote -v` which may list fetch+push).
func ToRemoteNameMap(remotes []storage.RemoteInfo) map[string]string {
	m := make(map[string]string, len(remotes))
	for _, r := range remotes {
		m[r.Name] = r.URL
	}
	return m
}
