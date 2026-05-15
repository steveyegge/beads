package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// stubDriver is a minimal no-op implementation of storage.Driver used only in
// registry tests. All Storage methods return errStubNotImpl.
type stubDriver struct{ name string }

var errStubNotImpl = errors.New("stub: not implemented")

func (d *stubDriver) Name() string                                         { return d.name }
func (d *stubDriver) Capabilities() storage.CapabilitySet                  { return storage.NewCapabilitySet() }
func (d *stubDriver) Open(_ context.Context, _ storage.DriverConfig) error { return nil }
func (d *stubDriver) Ping(_ context.Context) error                         { return errStubNotImpl }
func (d *stubDriver) SchemaVersion(_ context.Context) (int, error)         { return 0, errStubNotImpl }
func (d *stubDriver) InitSchema(_ context.Context) error                   { return errStubNotImpl }
func (d *stubDriver) MigrateSchema(_ context.Context, _ int) error         { return errStubNotImpl }
func (d *stubDriver) Close() error                                         { return nil }

// --- storage.Storage stubs ---
func (d *stubDriver) CreateIssue(_ context.Context, _ *types.Issue, _ string) error {
	return errStubNotImpl
}
func (d *stubDriver) CreateIssues(_ context.Context, _ []*types.Issue, _ string) error {
	return errStubNotImpl
}
func (d *stubDriver) GetIssue(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetIssueByExternalRef(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetIssuesByIDs(_ context.Context, _ []string) ([]*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) UpdateIssue(_ context.Context, _ string, _ map[string]interface{}, _ string) error {
	return errStubNotImpl
}
func (d *stubDriver) ReopenIssue(_ context.Context, _ string, _ string, _ string) error {
	return errStubNotImpl
}
func (d *stubDriver) UpdateIssueType(_ context.Context, _ string, _ string, _ string) error {
	return errStubNotImpl
}
func (d *stubDriver) CloseIssue(_ context.Context, _ string, _ string, _ string, _ string) error {
	return errStubNotImpl
}
func (d *stubDriver) DeleteIssue(_ context.Context, _ string) error { return errStubNotImpl }
func (d *stubDriver) SearchIssues(_ context.Context, _ string, _ types.IssueFilter) ([]*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) AddDependency(_ context.Context, _ *types.Dependency, _ string) error {
	return errStubNotImpl
}
func (d *stubDriver) RemoveDependency(_ context.Context, _, _ string, _ string) error {
	return errStubNotImpl
}
func (d *stubDriver) GetDependencies(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetDependents(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetDependenciesWithMetadata(_ context.Context, _ string) ([]*types.IssueWithDependencyMetadata, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetDependentsWithMetadata(_ context.Context, _ string) ([]*types.IssueWithDependencyMetadata, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetDependencyTree(_ context.Context, _ string, _ int, _ bool, _ bool) ([]*types.TreeNode, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) AddLabel(_ context.Context, _, _, _ string) error    { return errStubNotImpl }
func (d *stubDriver) RemoveLabel(_ context.Context, _, _, _ string) error { return errStubNotImpl }
func (d *stubDriver) GetLabels(_ context.Context, _ string) ([]string, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetIssuesByLabel(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetReadyWork(_ context.Context, _ types.WorkFilter) ([]*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetBlockedIssues(_ context.Context, _ types.WorkFilter) ([]*types.BlockedIssue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetEpicsEligibleForClosure(_ context.Context) ([]*types.EpicStatus, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) ListWisps(_ context.Context, _ types.WispFilter) ([]*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) AddIssueComment(_ context.Context, _, _, _ string) (*types.Comment, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetIssueComments(_ context.Context, _ string) ([]*types.Comment, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetEvents(_ context.Context, _ string, _ int) ([]*types.Event, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetAllEventsSince(_ context.Context, _ time.Time) ([]*types.Event, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) GetStatistics(_ context.Context) (*types.Statistics, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) SetConfig(_ context.Context, _, _ string) error { return errStubNotImpl }
func (d *stubDriver) GetConfig(_ context.Context, _ string) (string, error) {
	return "", errStubNotImpl
}
func (d *stubDriver) GetAllConfig(_ context.Context) (map[string]string, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) SetLocalMetadata(_ context.Context, _, _ string) error { return errStubNotImpl }
func (d *stubDriver) GetLocalMetadata(_ context.Context, _ string) (string, error) {
	return "", errStubNotImpl
}
func (d *stubDriver) RunInTransaction(_ context.Context, _ string, _ func(storage.Transaction) error) error {
	return errStubNotImpl
}
func (d *stubDriver) MergeSlotCreate(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) MergeSlotCheck(_ context.Context) (*storage.MergeSlotStatus, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) MergeSlotAcquire(_ context.Context, _, _ string, _ bool) (*storage.MergeSlotResult, error) {
	return nil, errStubNotImpl
}
func (d *stubDriver) MergeSlotRelease(_ context.Context, _, _ string) error { return errStubNotImpl }
func (d *stubDriver) SlotSet(_ context.Context, _, _, _, _ string) error    { return errStubNotImpl }
func (d *stubDriver) SlotGet(_ context.Context, _, _ string) (string, error) {
	return "", errStubNotImpl
}
func (d *stubDriver) SlotClear(_ context.Context, _, _, _ string) error { return errStubNotImpl }

// uniqueName returns a name unlikely to collide with real or parallel-test registrations.
func uniqueName(base string) string {
	return "test-" + base + "-" + time.Now().Format("20060102150405.999999999")
}

func TestRegisterDriver_PanicOnDuplicate(t *testing.T) {
	name := uniqueName("dup")
	opener := func() (storage.Driver, error) { return &stubDriver{name: name}, nil }
	storage.RegisterDriver(name, opener)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate RegisterDriver, got none")
		}
	}()
	storage.RegisterDriver(name, opener)
}

func TestOpenDriver_UnknownReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := storage.OpenDriver(ctx, uniqueName("unknown"), storage.DriverConfig{})
	if err == nil {
		t.Fatal("expected error for unknown driver, got nil")
	}
}

func TestOpenDriver_RoundTrip(t *testing.T) {
	ctx := context.Background()
	name := uniqueName("roundtrip")
	storage.RegisterDriver(name, func() (storage.Driver, error) {
		return &stubDriver{name: name}, nil
	})
	d, err := storage.OpenDriver(ctx, name, storage.DriverConfig{})
	if err != nil {
		t.Fatalf("OpenDriver(%q) error: %v", name, err)
	}
	if d.Name() != name {
		t.Errorf("driver name: got %q, want %q", d.Name(), name)
	}
}
