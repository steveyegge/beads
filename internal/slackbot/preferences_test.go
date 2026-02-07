package slackbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 1. IsValidNotificationLevel
// ---------------------------------------------------------------------------

func TestIsValidNotificationLevel(t *testing.T) {
	tests := []struct {
		level string
		want  bool
	}{
		{"all", true},
		{"high", true},
		{"muted", true},
		{"", false},
		{"HIGH", false},   // case-sensitive
		{"none", false},   // not a valid level
		{"All", false},    // mixed case
		{"medium", false}, // invented level
	}
	for _, tc := range tests {
		t.Run("level="+tc.level, func(t *testing.T) {
			got := IsValidNotificationLevel(tc.level)
			if got != tc.want {
				t.Errorf("IsValidNotificationLevel(%q) = %v, want %v", tc.level, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. DefaultUserPrefs
// ---------------------------------------------------------------------------

func TestDefaultUserPrefs(t *testing.T) {
	before := time.Now()
	prefs := DefaultUserPrefs()
	after := time.Now()

	if prefs.DMOptIn {
		t.Error("DMOptIn should default to false")
	}
	if prefs.NotificationLevel != "high" {
		t.Errorf("NotificationLevel = %q, want %q", prefs.NotificationLevel, "high")
	}
	if prefs.ThreadNotifications {
		t.Error("ThreadNotifications should default to false")
	}
	if prefs.UpdatedAt.Before(before) || prefs.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v, expected between %v and %v", prefs.UpdatedAt, before, after)
	}
}

// ---------------------------------------------------------------------------
// 3. NewPreferenceManager — file-path resolution
// ---------------------------------------------------------------------------

func TestNewPreferenceManager_ExplicitDir(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, "beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	pm := NewPreferenceManager(beadsDir)

	// With an explicit dir the file should live at beadsDir/../settings/slack_user_prefs.json
	want := filepath.Join(beadsDir, "..", "settings", "slack_user_prefs.json")
	if pm.GetFilePath() != want {
		t.Errorf("GetFilePath() = %q, want %q", pm.GetFilePath(), want)
	}
}

func TestNewPreferenceManager_EnvFallback(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, "from-env")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("BEADS_DIR", beadsDir)
	pm := NewPreferenceManager("")

	want := filepath.Join(beadsDir, "..", "settings", "slack_user_prefs.json")
	if pm.GetFilePath() != want {
		t.Errorf("GetFilePath() = %q, want %q", pm.GetFilePath(), want)
	}
}

func TestNewPreferenceManager_DotFallback(t *testing.T) {
	// Clear BEADS_DIR so the code falls back to ".".
	t.Setenv("BEADS_DIR", "")
	pm := NewPreferenceManager("")

	want := filepath.Join("settings", "slack_user_prefs.json")
	if pm.GetFilePath() != want {
		t.Errorf("GetFilePath() = %q, want %q", pm.GetFilePath(), want)
	}
}

// ---------------------------------------------------------------------------
// helpers to build an in-memory PreferenceManager without touching disk
// ---------------------------------------------------------------------------

func newTestPM(t *testing.T) *PreferenceManager {
	t.Helper()
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, "beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	return NewPreferenceManager(beadsDir)
}

// ---------------------------------------------------------------------------
// 4. GetUserPreferences
// ---------------------------------------------------------------------------

func TestGetUserPreferences_UnknownUser(t *testing.T) {
	pm := newTestPM(t)
	prefs := pm.GetUserPreferences("U_UNKNOWN")

	if prefs.DMOptIn {
		t.Error("unknown user DMOptIn should be false")
	}
	if prefs.NotificationLevel != "high" {
		t.Errorf("unknown user NotificationLevel = %q, want %q", prefs.NotificationLevel, "high")
	}
	if prefs.ThreadNotifications {
		t.Error("unknown user ThreadNotifications should be false")
	}
}

func TestGetUserPreferences_KnownUser(t *testing.T) {
	pm := newTestPM(t)
	if err := pm.SetDMOptIn("U1", true); err != nil {
		t.Fatal(err)
	}

	prefs := pm.GetUserPreferences("U1")
	if !prefs.DMOptIn {
		t.Error("expected DMOptIn=true after SetDMOptIn(true)")
	}
}

// ---------------------------------------------------------------------------
// 5. SetDMOptIn
// ---------------------------------------------------------------------------

func TestSetDMOptIn(t *testing.T) {
	pm := newTestPM(t)

	// Set true for a new user.
	if err := pm.SetDMOptIn("U1", true); err != nil {
		t.Fatal(err)
	}
	if !pm.GetUserPreferences("U1").DMOptIn {
		t.Error("DMOptIn should be true")
	}

	// Toggle to false.
	if err := pm.SetDMOptIn("U1", false); err != nil {
		t.Fatal(err)
	}
	if pm.GetUserPreferences("U1").DMOptIn {
		t.Error("DMOptIn should be false after toggling off")
	}

	// Set for a brand-new user.
	if err := pm.SetDMOptIn("U2", true); err != nil {
		t.Fatal(err)
	}
	if !pm.GetUserPreferences("U2").DMOptIn {
		t.Error("new user U2 DMOptIn should be true")
	}
}

// ---------------------------------------------------------------------------
// 6. SetNotificationLevel
// ---------------------------------------------------------------------------

func TestSetNotificationLevel(t *testing.T) {
	pm := newTestPM(t)

	t.Run("valid_all", func(t *testing.T) {
		if err := pm.SetNotificationLevel("U1", "all"); err != nil {
			t.Fatal(err)
		}
		if got := pm.GetUserPreferences("U1").NotificationLevel; got != "all" {
			t.Errorf("NotificationLevel = %q, want %q", got, "all")
		}
	})

	t.Run("valid_muted", func(t *testing.T) {
		if err := pm.SetNotificationLevel("U1", "muted"); err != nil {
			t.Fatal(err)
		}
		if got := pm.GetUserPreferences("U1").NotificationLevel; got != "muted" {
			t.Errorf("NotificationLevel = %q, want %q", got, "muted")
		}
	})

	t.Run("invalid_level", func(t *testing.T) {
		err := pm.SetNotificationLevel("U1", "invalid")
		if err == nil {
			t.Fatal("expected error for invalid level")
		}
	})

	t.Run("updatedAt_changes", func(t *testing.T) {
		if err := pm.SetNotificationLevel("U2", "high"); err != nil {
			t.Fatal(err)
		}
		before := pm.GetUserPreferences("U2").UpdatedAt

		// Sleep briefly so timestamps differ.
		time.Sleep(5 * time.Millisecond)

		if err := pm.SetNotificationLevel("U2", "all"); err != nil {
			t.Fatal(err)
		}
		after := pm.GetUserPreferences("U2").UpdatedAt

		if !after.After(before) {
			t.Errorf("UpdatedAt did not advance: before=%v, after=%v", before, after)
		}
	})
}

// ---------------------------------------------------------------------------
// 7. SetThreadNotifications
// ---------------------------------------------------------------------------

func TestSetThreadNotifications(t *testing.T) {
	pm := newTestPM(t)

	if err := pm.SetThreadNotifications("U1", true); err != nil {
		t.Fatal(err)
	}
	if !pm.GetUserPreferences("U1").ThreadNotifications {
		t.Error("ThreadNotifications should be true")
	}

	if err := pm.SetThreadNotifications("U1", false); err != nil {
		t.Fatal(err)
	}
	if pm.GetUserPreferences("U1").ThreadNotifications {
		t.Error("ThreadNotifications should be false after toggling off")
	}
}

// ---------------------------------------------------------------------------
// 8. IsEligibleForDM
// ---------------------------------------------------------------------------

func TestIsEligibleForDM(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(pm *PreferenceManager)
		userID  string
		want    bool
	}{
		{
			name:   "unknown_user",
			setup:  func(pm *PreferenceManager) {},
			userID: "U_UNKNOWN",
			want:   false,
		},
		{
			name: "opted_in_level_all",
			setup: func(pm *PreferenceManager) {
				pm.SetDMOptIn("U1", true)
				pm.SetNotificationLevel("U1", "all")
			},
			userID: "U1",
			want:   true,
		},
		{
			name: "opted_in_level_high",
			setup: func(pm *PreferenceManager) {
				pm.SetDMOptIn("U1", true)
				pm.SetNotificationLevel("U1", "high")
			},
			userID: "U1",
			want:   true,
		},
		{
			name: "opted_in_level_muted",
			setup: func(pm *PreferenceManager) {
				pm.SetDMOptIn("U1", true)
				pm.SetNotificationLevel("U1", "muted")
			},
			userID: "U1",
			want:   false,
		},
		{
			name: "opted_out_level_all",
			setup: func(pm *PreferenceManager) {
				pm.SetDMOptIn("U1", false)
				pm.SetNotificationLevel("U1", "all")
			},
			userID: "U1",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pm := newTestPM(t)
			tc.setup(pm)
			got := pm.IsEligibleForDM(tc.userID)
			if got != tc.want {
				t.Errorf("IsEligibleForDM(%q) = %v, want %v", tc.userID, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 9. UserCount
// ---------------------------------------------------------------------------

func TestUserCount(t *testing.T) {
	pm := newTestPM(t)

	if pm.UserCount() != 0 {
		t.Errorf("empty manager UserCount = %d, want 0", pm.UserCount())
	}

	pm.SetDMOptIn("U1", true)
	pm.SetDMOptIn("U2", false)

	if pm.UserCount() != 2 {
		t.Errorf("UserCount = %d, want 2", pm.UserCount())
	}
}

// ---------------------------------------------------------------------------
// 10. ListUsers
// ---------------------------------------------------------------------------

func TestListUsers(t *testing.T) {
	pm := newTestPM(t)

	if users := pm.ListUsers(); len(users) != 0 {
		t.Errorf("empty manager ListUsers = %v, want empty", users)
	}

	pm.SetDMOptIn("U2", true)
	pm.SetDMOptIn("U1", true)
	pm.SetDMOptIn("U3", true)

	users := pm.ListUsers()
	sort.Strings(users) // map iteration order is nondeterministic
	want := []string{"U1", "U2", "U3"}
	if len(users) != len(want) {
		t.Fatalf("ListUsers len = %d, want %d", len(users), len(want))
	}
	for i := range want {
		if users[i] != want[i] {
			t.Errorf("ListUsers[%d] = %q, want %q", i, users[i], want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// 11. ClearUserPreferences
// ---------------------------------------------------------------------------

func TestClearUserPreferences(t *testing.T) {
	pm := newTestPM(t)

	pm.SetDMOptIn("U1", true)
	pm.SetNotificationLevel("U1", "all")

	if pm.UserCount() != 1 {
		t.Fatalf("expected 1 user, got %d", pm.UserCount())
	}

	pm.ClearUserPreferences("U1")

	if pm.UserCount() != 0 {
		t.Errorf("after clear UserCount = %d, want 0", pm.UserCount())
	}
	// After clearing, should get defaults again.
	prefs := pm.GetUserPreferences("U1")
	if prefs.DMOptIn {
		t.Error("cleared user should get default DMOptIn=false")
	}
	if prefs.NotificationLevel != "high" {
		t.Errorf("cleared user NotificationLevel = %q, want %q", prefs.NotificationLevel, "high")
	}
}

func TestClearUserPreferences_Nonexistent(t *testing.T) {
	pm := newTestPM(t)
	// Should not panic or error when clearing a user that doesn't exist.
	pm.ClearUserPreferences("U_NONEXISTENT")
	if pm.UserCount() != 0 {
		t.Errorf("UserCount = %d, want 0", pm.UserCount())
	}
}

// ---------------------------------------------------------------------------
// 12. Save / Load round-trip
// ---------------------------------------------------------------------------

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, "beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Build up state in the first manager.
	pm1 := NewPreferenceManager(beadsDir)
	pm1.SetDMOptIn("U1", true)
	pm1.SetNotificationLevel("U1", "all")
	pm1.SetThreadNotifications("U1", true)
	pm1.SetDMOptIn("U2", false)
	pm1.SetNotificationLevel("U2", "muted")

	if err := pm1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Create a second manager pointing at the same directory and load.
	pm2 := &PreferenceManager{
		prefs:    make(map[string]UserPrefs),
		filePath: pm1.GetFilePath(),
	}
	if err := pm2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify user count.
	if pm2.UserCount() != 2 {
		t.Fatalf("loaded UserCount = %d, want 2", pm2.UserCount())
	}

	// Verify U1 prefs.
	u1 := pm2.GetUserPreferences("U1")
	if !u1.DMOptIn {
		t.Error("loaded U1 DMOptIn should be true")
	}
	if u1.NotificationLevel != "all" {
		t.Errorf("loaded U1 NotificationLevel = %q, want %q", u1.NotificationLevel, "all")
	}
	if !u1.ThreadNotifications {
		t.Error("loaded U1 ThreadNotifications should be true")
	}

	// Verify U2 prefs.
	u2 := pm2.GetUserPreferences("U2")
	if u2.DMOptIn {
		t.Error("loaded U2 DMOptIn should be false")
	}
	if u2.NotificationLevel != "muted" {
		t.Errorf("loaded U2 NotificationLevel = %q, want %q", u2.NotificationLevel, "muted")
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	pm := &PreferenceManager{
		prefs:    make(map[string]UserPrefs),
		filePath: filepath.Join(t.TempDir(), "does_not_exist.json"),
	}

	// Load from a file that doesn't exist should return nil (fresh install).
	if err := pm.Load(); err != nil {
		t.Errorf("Load from non-existent file should return nil, got: %v", err)
	}
	if pm.UserCount() != 0 {
		t.Errorf("UserCount after loading empty = %d, want 0", pm.UserCount())
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(fp, []byte("{not-json!}"), 0644); err != nil {
		t.Fatal(err)
	}

	pm := &PreferenceManager{
		prefs:    make(map[string]UserPrefs),
		filePath: fp,
	}

	if err := pm.Load(); err == nil {
		t.Error("Load from invalid JSON should return error")
	}
}

func TestSave_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	// Point at a deeply nested path that doesn't exist yet.
	fp := filepath.Join(dir, "deep", "nested", "settings", "prefs.json")

	pm := &PreferenceManager{
		prefs:    make(map[string]UserPrefs),
		filePath: fp,
	}
	pm.prefs["U1"] = DefaultUserPrefs()

	if err := pm.Save(); err != nil {
		t.Fatalf("Save should create intermediate dirs: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		t.Error("preferences file was not created")
	}
}

// ---------------------------------------------------------------------------
// 13. Save produces valid JSON
// ---------------------------------------------------------------------------

func TestSave_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, "beads")
	os.MkdirAll(beadsDir, 0755)

	pm := NewPreferenceManager(beadsDir)
	pm.SetDMOptIn("U1", true)
	pm.SetNotificationLevel("U1", "all")

	if err := pm.Save(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(pm.GetFilePath())
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("saved file is not valid JSON: %v", err)
	}
	if _, ok := raw["U1"]; !ok {
		t.Error("saved JSON missing key U1")
	}
}

// ---------------------------------------------------------------------------
// 14. Concurrent access safety (smoke test)
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	pm := newTestPM(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			pm.SetDMOptIn("U1", true)
			pm.SetNotificationLevel("U1", "all")
			pm.SetThreadNotifications("U1", true)
		}
	}()

	for i := 0; i < 100; i++ {
		pm.GetUserPreferences("U1")
		pm.IsEligibleForDM("U1")
		pm.UserCount()
		pm.ListUsers()
	}
	<-done
}
