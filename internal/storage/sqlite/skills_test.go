package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestUpsertSkill_Basic(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	skill := &Skill{
		ID:     "tdd",
		Name:   "Test-Driven Development",
		Source: "claude",
		Path:   "CLAUDE.md#tdd",
		Tier:   "must-have",
		SHA256: "abc123def456",
		Bytes:  1024,
		Status: "active",
	}

	// Insert
	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("UpsertSkill failed: %v", err)
	}

	// Verify
	got, err := store.GetSkill(ctx, "tdd")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetSkill returned nil")
	}
	if got.ID != "tdd" {
		t.Errorf("ID = %q, want %q", got.ID, "tdd")
	}
	if got.Name != "Test-Driven Development" {
		t.Errorf("Name = %q, want %q", got.Name, "Test-Driven Development")
	}
	if got.Source != "claude" {
		t.Errorf("Source = %q, want %q", got.Source, "claude")
	}
	if got.Tier != "must-have" {
		t.Errorf("Tier = %q, want %q", got.Tier, "must-have")
	}
	if got.SHA256 != "abc123def456" {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, "abc123def456")
	}
}

func TestUpsertSkill_Update(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert initial
	skill := &Skill{
		ID:     "debug",
		Name:   "Debugging",
		Source: "codex",
		Tier:   "optional",
		SHA256: "hash1",
		Status: "active",
	}
	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("Initial UpsertSkill failed: %v", err)
	}

	// Update with new hash (simulating content change)
	skill.SHA256 = "hash2"
	skill.Name = "Advanced Debugging"
	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("Update UpsertSkill failed: %v", err)
	}

	// Verify update
	got, err := store.GetSkill(ctx, "debug")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got.SHA256 != "hash2" {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, "hash2")
	}
	if got.Name != "Advanced Debugging" {
		t.Errorf("Name = %q, want %q", got.Name, "Advanced Debugging")
	}
}

func TestUpsertSkill_Validation(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	tests := []struct {
		name    string
		skill   *Skill
		wantErr bool
	}{
		{
			name:    "nil skill",
			skill:   nil,
			wantErr: true,
		},
		{
			name:    "missing id",
			skill:   &Skill{Name: "Test", Source: "claude", SHA256: "abc"},
			wantErr: true,
		},
		{
			name:    "missing name",
			skill:   &Skill{ID: "test", Source: "claude", SHA256: "abc"},
			wantErr: true,
		},
		{
			name:    "missing source",
			skill:   &Skill{ID: "test", Name: "Test", SHA256: "abc"},
			wantErr: true,
		},
		{
			name:    "missing sha256",
			skill:   &Skill{ID: "test", Name: "Test", Source: "claude"},
			wantErr: true,
		},
		{
			name:    "invalid source",
			skill:   &Skill{ID: "test", Name: "Test", Source: "invalid", SHA256: "abc"},
			wantErr: true,
		},
		{
			name:    "invalid tier",
			skill:   &Skill{ID: "test", Name: "Test", Source: "claude", SHA256: "abc", Tier: "invalid"},
			wantErr: true,
		},
		{
			name:    "invalid status",
			skill:   &Skill{ID: "test", Name: "Test", Source: "claude", SHA256: "abc", Status: "invalid"},
			wantErr: true,
		},
		{
			name:    "valid minimal",
			skill:   &Skill{ID: "test", Name: "Test", Source: "claude", SHA256: "abc"},
			wantErr: false,
		},
		{
			name:    "valid opencode source",
			skill:   &Skill{ID: "test2", Name: "Test2", Source: "opencode", SHA256: "abc"},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := store.UpsertSkill(ctx, tc.skill)
			if (err != nil) != tc.wantErr {
				t.Errorf("UpsertSkill() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestGetSkill_NotFound(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	got, err := store.GetSkill(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent skill, got %+v", got)
	}
}

func TestListSkills_NoFilter(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert skills
	skills := []*Skill{
		{ID: "skill1", Name: "Skill One", Source: "claude", SHA256: "h1"},
		{ID: "skill2", Name: "Skill Two", Source: "codex", SHA256: "h2"},
		{ID: "skill3", Name: "Skill Three", Source: "opencode", SHA256: "h3"},
	}
	for _, s := range skills {
		if err := store.UpsertSkill(ctx, s); err != nil {
			t.Fatalf("UpsertSkill failed: %v", err)
		}
	}

	// List all
	got, err := store.ListSkills(ctx, SkillFilter{})
	if err != nil {
		t.Fatalf("ListSkills failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d skills, want 3", len(got))
	}
}

func TestListSkills_WithFilter(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert skills with different attributes
	skills := []*Skill{
		{ID: "s1", Name: "S1", Source: "claude", Tier: "must-have", Status: "active", SHA256: "h1"},
		{ID: "s2", Name: "S2", Source: "claude", Tier: "optional", Status: "active", SHA256: "h2"},
		{ID: "s3", Name: "S3", Source: "codex", Tier: "must-have", Status: "deprecated", SHA256: "h3"},
		{ID: "s4", Name: "S4", Source: "codex", Tier: "optional", Status: "archived", SHA256: "h4"},
	}
	for _, s := range skills {
		if err := store.UpsertSkill(ctx, s); err != nil {
			t.Fatalf("UpsertSkill failed: %v", err)
		}
	}

	tests := []struct {
		name   string
		filter SkillFilter
		want   int
	}{
		{"filter by source claude", SkillFilter{Source: "claude"}, 2},
		{"filter by source codex", SkillFilter{Source: "codex"}, 2},
		{"filter by tier must-have", SkillFilter{Tier: "must-have"}, 2},
		{"filter by status active", SkillFilter{Status: "active"}, 2},
		{"filter by status archived", SkillFilter{Status: "archived"}, 1},
		{"filter by source and tier", SkillFilter{Source: "claude", Tier: "must-have"}, 1},
		{"filter by all", SkillFilter{Source: "codex", Tier: "optional", Status: "archived"}, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.ListSkills(ctx, tc.filter)
			if err != nil {
				t.Fatalf("ListSkills failed: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("got %d skills, want %d", len(got), tc.want)
			}
		})
	}
}

func TestLinkSkillToBead(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a skill
	skill := &Skill{ID: "tdd", Name: "TDD", Source: "claude", SHA256: "h1"}
	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("UpsertSkill failed: %v", err)
	}

	// Create a bead (issue)
	env := &testEnv{t: t, Store: store, Ctx: ctx}
	issue := env.CreateIssue("Test issue")

	// Link skill to bead
	if err := store.LinkSkillToBead(ctx, "tdd", issue.ID); err != nil {
		t.Fatalf("LinkSkillToBead failed: %v", err)
	}

	// Verify link
	beads, err := store.GetSkillBeads(ctx, "tdd")
	if err != nil {
		t.Fatalf("GetSkillBeads failed: %v", err)
	}
	if len(beads) != 1 {
		t.Errorf("got %d beads, want 1", len(beads))
	}
	if beads[0] != issue.ID {
		t.Errorf("bead ID = %q, want %q", beads[0], issue.ID)
	}

	// Verify last_used_at was updated
	got, err := store.GetSkill(ctx, "tdd")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got.LastUsedAt == nil {
		t.Error("expected last_used_at to be set")
	}
}

func TestLinkSkillToBead_Duplicate(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create skill and bead
	skill := &Skill{ID: "tdd", Name: "TDD", Source: "claude", SHA256: "h1"}
	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("UpsertSkill failed: %v", err)
	}
	env := &testEnv{t: t, Store: store, Ctx: ctx}
	issue := env.CreateIssue("Test issue")

	// Link twice (should not error)
	if err := store.LinkSkillToBead(ctx, "tdd", issue.ID); err != nil {
		t.Fatalf("First LinkSkillToBead failed: %v", err)
	}
	if err := store.LinkSkillToBead(ctx, "tdd", issue.ID); err != nil {
		t.Fatalf("Second LinkSkillToBead failed: %v", err)
	}

	// Should still only have one link
	beads, err := store.GetSkillBeads(ctx, "tdd")
	if err != nil {
		t.Fatalf("GetSkillBeads failed: %v", err)
	}
	if len(beads) != 1 {
		t.Errorf("got %d beads, want 1 (duplicate link should be ignored)", len(beads))
	}
}

func TestUnlinkSkillFromBead(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create skill and bead
	skill := &Skill{ID: "tdd", Name: "TDD", Source: "claude", SHA256: "h1"}
	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("UpsertSkill failed: %v", err)
	}
	env := &testEnv{t: t, Store: store, Ctx: ctx}
	issue := env.CreateIssue("Test issue")

	// Link and then unlink
	if err := store.LinkSkillToBead(ctx, "tdd", issue.ID); err != nil {
		t.Fatalf("LinkSkillToBead failed: %v", err)
	}
	if err := store.UnlinkSkillFromBead(ctx, "tdd", issue.ID); err != nil {
		t.Fatalf("UnlinkSkillFromBead failed: %v", err)
	}

	// Verify unlinked
	beads, err := store.GetSkillBeads(ctx, "tdd")
	if err != nil {
		t.Fatalf("GetSkillBeads failed: %v", err)
	}
	if len(beads) != 0 {
		t.Errorf("got %d beads, want 0 after unlink", len(beads))
	}
}

func TestGetBeadSkills(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create skills
	skills := []*Skill{
		{ID: "tdd", Name: "TDD", Source: "claude", SHA256: "h1"},
		{ID: "debug", Name: "Debug", Source: "codex", SHA256: "h2"},
		{ID: "review", Name: "Review", Source: "opencode", SHA256: "h3"},
	}
	for _, s := range skills {
		if err := store.UpsertSkill(ctx, s); err != nil {
			t.Fatalf("UpsertSkill failed: %v", err)
		}
	}

	// Create bead and link skills
	env := &testEnv{t: t, Store: store, Ctx: ctx}
	issue := env.CreateIssue("Test issue")

	if err := store.LinkSkillToBead(ctx, "tdd", issue.ID); err != nil {
		t.Fatalf("LinkSkillToBead tdd failed: %v", err)
	}
	if err := store.LinkSkillToBead(ctx, "debug", issue.ID); err != nil {
		t.Fatalf("LinkSkillToBead debug failed: %v", err)
	}
	// Don't link "review"

	// Get bead skills
	got, err := store.GetBeadSkills(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetBeadSkills failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d skills, want 2", len(got))
	}

	// Check skills are sorted by name (Debug before TDD)
	if len(got) >= 2 {
		if got[0].ID != "debug" {
			t.Errorf("first skill ID = %q, want %q (sorted by name)", got[0].ID, "debug")
		}
		if got[1].ID != "tdd" {
			t.Errorf("second skill ID = %q, want %q (sorted by name)", got[1].ID, "tdd")
		}
	}
}

func TestGetUnusedSkills(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create skills
	skills := []*Skill{
		{ID: "used", Name: "Used Skill", Source: "claude", SHA256: "h1"},
		{ID: "unused1", Name: "Unused One", Source: "codex", SHA256: "h2"},
		{ID: "unused2", Name: "Unused Two", Source: "opencode", SHA256: "h3"},
	}
	for _, s := range skills {
		if err := store.UpsertSkill(ctx, s); err != nil {
			t.Fatalf("UpsertSkill failed: %v", err)
		}
	}

	// Link one skill to a bead
	env := &testEnv{t: t, Store: store, Ctx: ctx}
	issue := env.CreateIssue("Test issue")
	if err := store.LinkSkillToBead(ctx, "used", issue.ID); err != nil {
		t.Fatalf("LinkSkillToBead failed: %v", err)
	}

	// Get unused skills
	unused, err := store.GetUnusedSkills(ctx)
	if err != nil {
		t.Fatalf("GetUnusedSkills failed: %v", err)
	}
	if len(unused) != 2 {
		t.Errorf("got %d unused skills, want 2", len(unused))
	}

	// Verify the "used" skill is not in the list
	for _, s := range unused {
		if s.ID == "used" {
			t.Error("found 'used' skill in unused list")
		}
	}
}

func TestDeleteSkill(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create and delete skill
	skill := &Skill{ID: "todelete", Name: "To Delete", Source: "claude", SHA256: "h1"}
	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("UpsertSkill failed: %v", err)
	}
	if err := store.DeleteSkill(ctx, "todelete"); err != nil {
		t.Fatalf("DeleteSkill failed: %v", err)
	}

	// Verify deleted
	got, err := store.GetSkill(ctx, "todelete")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got != nil {
		t.Error("expected skill to be deleted")
	}
}

func TestDeleteSkill_NotFound(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	err := store.DeleteSkill(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for deleting nonexistent skill")
	}
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestUpdateSkillStatus(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create skill
	skill := &Skill{ID: "test", Name: "Test", Source: "claude", SHA256: "h1", Status: "active"}
	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("UpsertSkill failed: %v", err)
	}

	// Archive it
	if err := store.UpdateSkillStatus(ctx, "test", "archived"); err != nil {
		t.Fatalf("UpdateSkillStatus to archived failed: %v", err)
	}

	// Verify status and archived_at
	got, err := store.GetSkill(ctx, "test")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got.Status != "archived" {
		t.Errorf("status = %q, want %q", got.Status, "archived")
	}
	if got.ArchivedAt == nil {
		t.Error("expected archived_at to be set")
	}

	// Reactivate it
	if err := store.UpdateSkillStatus(ctx, "test", "active"); err != nil {
		t.Fatalf("UpdateSkillStatus to active failed: %v", err)
	}

	// Verify archived_at is cleared
	got, err = store.GetSkill(ctx, "test")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want %q", got.Status, "active")
	}
	if got.ArchivedAt != nil {
		t.Error("expected archived_at to be cleared after reactivation")
	}
}

func TestGetSkillUsageStats(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create skills
	skills := []*Skill{
		{ID: "popular", Name: "Popular", Source: "claude", SHA256: "h1"},
		{ID: "unpopular", Name: "Unpopular", Source: "codex", SHA256: "h2"},
		{ID: "unused", Name: "Unused", Source: "opencode", SHA256: "h3"},
	}
	for _, s := range skills {
		if err := store.UpsertSkill(ctx, s); err != nil {
			t.Fatalf("UpsertSkill failed: %v", err)
		}
	}

	// Create beads and link skills
	env := &testEnv{t: t, Store: store, Ctx: ctx}
	issue1 := env.CreateIssue("Issue 1")
	issue2 := env.CreateIssue("Issue 2")
	issue3 := env.CreateIssue("Issue 3")

	// "popular" linked to 3 beads
	store.LinkSkillToBead(ctx, "popular", issue1.ID)
	store.LinkSkillToBead(ctx, "popular", issue2.ID)
	store.LinkSkillToBead(ctx, "popular", issue3.ID)

	// "unpopular" linked to 1 bead
	store.LinkSkillToBead(ctx, "unpopular", issue1.ID)

	// "unused" not linked

	// Get stats
	stats, err := store.GetSkillUsageStats(ctx)
	if err != nil {
		t.Fatalf("GetSkillUsageStats failed: %v", err)
	}

	if stats["popular"] != 3 {
		t.Errorf("popular count = %d, want 3", stats["popular"])
	}
	if stats["unpopular"] != 1 {
		t.Errorf("unpopular count = %d, want 1", stats["unpopular"])
	}
	if _, exists := stats["unused"]; exists {
		t.Error("unused skill should not appear in stats")
	}
}

func TestBulkUpsertSkills(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	skills := []*Skill{
		{ID: "s1", Name: "Skill 1", Source: "claude", SHA256: "h1"},
		{ID: "s2", Name: "Skill 2", Source: "codex", SHA256: "h2"},
		{ID: "s3", Name: "Skill 3", Source: "opencode", SHA256: "h3"},
	}

	if err := store.BulkUpsertSkills(ctx, skills); err != nil {
		t.Fatalf("BulkUpsertSkills failed: %v", err)
	}

	// Verify all inserted
	got, err := store.ListSkills(ctx, SkillFilter{})
	if err != nil {
		t.Fatalf("ListSkills failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d skills, want 3", len(got))
	}
}

func TestBulkLinkSkillsToBeads(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create skills
	skills := []*Skill{
		{ID: "s1", Name: "Skill 1", Source: "claude", SHA256: "h1"},
		{ID: "s2", Name: "Skill 2", Source: "codex", SHA256: "h2"},
		{ID: "s3", Name: "Skill 3", Source: "opencode", SHA256: "h3"},
	}
	if err := store.BulkUpsertSkills(ctx, skills); err != nil {
		t.Fatalf("BulkUpsertSkills failed: %v", err)
	}

	// Create bead
	env := &testEnv{t: t, Store: store, Ctx: ctx}
	issue := env.CreateIssue("Test issue")

	// Bulk link
	if err := store.BulkLinkSkillsToBeads(ctx, issue.ID, []string{"s1", "s2", "s3"}); err != nil {
		t.Fatalf("BulkLinkSkillsToBeads failed: %v", err)
	}

	// Verify all linked
	got, err := store.GetBeadSkills(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetBeadSkills failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d skills, want 3", len(got))
	}

	// Verify last_used_at was updated for all
	for _, skillID := range []string{"s1", "s2", "s3"} {
		skill, err := store.GetSkill(ctx, skillID)
		if err != nil {
			t.Fatalf("GetSkill %s failed: %v", skillID, err)
		}
		if skill.LastUsedAt == nil {
			t.Errorf("skill %s: expected last_used_at to be set", skillID)
		}
	}
}

func TestSkillTimestamps(t *testing.T) {
	ctx := context.Background()
	store, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)

	skill := &Skill{
		ID:        "test",
		Name:      "Test Skill",
		Source:    "claude",
		SHA256:    "h1",
		CreatedAt: now,
	}

	if err := store.UpsertSkill(ctx, skill); err != nil {
		t.Fatalf("UpsertSkill failed: %v", err)
	}

	got, err := store.GetSkill(ctx, "test")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}

	// Check created_at was preserved
	if got.CreatedAt.Unix() != now.Unix() {
		t.Errorf("created_at = %v, want %v", got.CreatedAt.Unix(), now.Unix())
	}

	// last_used_at should be nil initially
	if got.LastUsedAt != nil {
		t.Error("expected last_used_at to be nil initially")
	}

	// archived_at should be nil initially
	if got.ArchivedAt != nil {
		t.Error("expected archived_at to be nil initially")
	}
}
