package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
)

// The resolver's own tests -- the table of ways a store can fail to answer, how the
// stored string is read back, the capability guard, the recover, and the raw-store
// ordering -- moved to internal/versionedhistory with the resolver, assertions
// unchanged, which is what shows the move preserved #6661's behavior. What stays
// here is the one thing that is cmd/bd's: how the decorator chain is wired.

// configProbeStore is a raw store at the bottom of a decorator chain whose
// settings plane answers however a case needs. It counts the reads so a test can
// prove it was actually asked -- or, here, that it was NOT.
//
// Every other method promotes from the embedded nil interface and panics if
// called, which is deliberate: nothing here should be calling them.
type configProbeStore struct {
	storage.DoltStorage
	cfg     issueops.WorkspaceConfig
	calls   int
	enabled bool
}

func (s *configProbeStore) WorkspaceConfig() (issueops.WorkspaceConfig, error) {
	s.calls++
	return s.cfg, nil
}

// SetVersionedHistoryEnabled makes this store a storage.VersionedHistoryConfigurer,
// which is what would earn it a store-plane read if wiring still did one.
func (s *configProbeStore) SetVersionedHistoryEnabled(enabled bool) { s.enabled = enabled }

// settingsPlane is a workspace settings plane with one canned answer.
type settingsPlane struct {
	issueops.WorkspaceConfig
	value string
}

func (p *settingsPlane) GetSetting(context.Context, issueops.GetSettingRequest) (issueops.SettingResult, error) {
	return issueops.SettingResult{Key: versionedHistorySettingKey, Value: p.value}, nil
}

// TestWireStorageDecoratorsLeavesActivationToTheFactories pins the other half of
// moving activation to where a store is constructed.
//
// wireStorageDecorators used to be the one place the switch was read and applied,
// and it runs on the ONE store a command opens for its own workspace. Every store
// a routed update, a routed close or `bd create --repo` opens for ANOTHER
// workspace is built by newDoltStoreFromConfig and never passed through here, so
// it recorded nothing while the command reported success. Activation now happens
// in the factories, for every store they hand back; if wiring ALSO read the row it
// would read it twice per open, and would read as the place that activates -- the
// misreading that produced the defect.
//
// The count is the assertion that matters, not "it survived": the store here would
// answer true, and would be switched on, if wiring still consulted it.
func TestWireStorageDecoratorsLeavesActivationToTheFactories(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  string
	}{
		{"the store's row says on", ""},
		{"the environment says on", "1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clearTelemetryEnv(t)
			t.Setenv("BD_VERSIONED_HISTORY_ENABLED", tt.env)
			// config.GetBool answers false until viper is initialised, so without
			// this the env subtest would pass whatever wiring did with the env.
			if err := config.Initialize(); err != nil {
				t.Fatalf("config.Initialize(): %v", err)
			}
			t.Cleanup(func() { _ = config.Initialize() })

			raw := &configProbeStore{cfg: &settingsPlane{value: "true"}}
			chain := wireStorageDecorators(raw, hooks.NewRunner("/nonexistent"), false)

			if chain == nil {
				t.Fatal("wireStorageDecorators returned nil for a non-nil store")
			}
			if raw.calls != 0 {
				t.Errorf("wireStorageDecorators read the store's settings plane %d time(s), want 0: activation belongs to the factory that constructed the store", raw.calls)
			}
			if raw.enabled {
				t.Error("wireStorageDecorators switched versioned history on; only the factory that constructs a store may, at construction")
			}
		})
	}
}

// scanStore is a raw store whose issue reader answers the pre-enable scan's one
// listing with canned rows. It cannot say which rows participate in history, so it
// is the backend the scan has to over-approximate on. Every other method promotes
// from the embedded nil interface and panics if called.
type scanStore struct {
	storage.DoltStorage
	rows []*types.Issue
}

func (s *scanStore) IssueReader() (issueops.Reader, error) { return scanReader{rows: s.rows}, nil }

type scanReader struct {
	issueops.Reader
	rows []*types.Issue
}

func (r scanReader) List(context.Context, issueops.ListRequest) (issueops.IssuePage, error) {
	items := make([]*issueops.IssueWithCounts, 0, len(r.rows))
	for _, issue := range r.rows {
		items = append(items, &issueops.IssueWithCounts{Issue: issue})
	}
	return issueops.IssuePage{Items: items}, nil
}

// participationScanStore is a scanStore that also answers
// storage.HistoryParticipation, and records what it was asked.
type participationScanStore struct {
	scanStore
	participating map[string]bool
	err           error
	asked         [][]string
}

func (s *participationScanStore) ParticipatingIssueIDs(_ context.Context, ids []string) (map[string]struct{}, error) {
	s.asked = append(s.asked, append([]string(nil), ids...))
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]struct{}{}
	for _, id := range ids {
		if s.participating[id] {
			out[id] = struct{}{}
		}
	}
	return out, nil
}

// TestVersionedHistorySwitchScanNamesOnlyRowsThatParticipate pins the precision of
// the pre-enable scan on the direct route, in process, against the decision it makes.
//
// Design §16.2b's write fence skips a legacy row (participation_generation NULL)
// before the mint looks at it, so a value a legacy row holds can never make a write
// fail, and the scan must not refuse the switch over it: on a store that has never
// recorded, every row is legacy. The scan reads rows, which do not carry
// participation, finds the ones a version would refuse, asks the store which of those
// participate (storage.HistoryParticipation, peeled out of the decorator chain), and
// keeps only them. It asks about the offenders alone, and a clean store is asked
// nothing. A store that cannot answer keeps every offender, which can only refuse
// more, and the refusal says it could not tell. A participation read that fails
// refuses, because a check that could not run has not passed.
func TestVersionedHistorySwitchScanNamesOnlyRowsThatParticipate(t *testing.T) {
	big := json.RawMessage(`{"ts":1727000000000000000}`)
	legacy := &types.Issue{ID: "vs-legacy", Metadata: big}
	participating := &types.Issue{ID: "vs-part", Metadata: big}
	gate := &types.Issue{ID: "vs-gate", IssueType: types.TypeGate, Timeout: 2600 * time.Hour}
	clean := &types.Issue{ID: "vs-clean", Metadata: json.RawMessage(`{"ok":1}`)}
	wisp := &types.Issue{ID: "vs-wisp", Ephemeral: true, Metadata: big}
	rows := []*types.Issue{legacy, participating, gate, clean, wisp}
	inStore := map[string]bool{participating.ID: true, gate.ID: true, clean.ID: true}

	withStore := func(t *testing.T, st storage.DoltStorage) {
		t.Helper()
		oldStore, oldProxied := store, proxiedServerMode
		store, proxiedServerMode = st, false
		t.Cleanup(func() { store, proxiedServerMode = oldStore, oldProxied })
	}
	enable := func() error {
		return checkVersionedHistoryCanBeEnabled(context.Background(), versionedHistorySettingKey, "true")
	}

	t.Run("names the participating offenders and not the legacy one", func(t *testing.T) {
		st := &participationScanStore{scanStore: scanStore{rows: rows}, participating: inStore}
		withStore(t, st)
		err := enable()
		if err == nil {
			t.Fatal("the switch was allowed over a participating row a version could not record")
		}
		out := err.Error()
		for _, id := range []string{participating.ID, gate.ID} {
			if !strings.Contains(out, id) {
				t.Errorf("the refusal does not name %s, which participates and holds a value a version could not record:\n%s", id, out)
			}
		}
		for _, id := range []string{legacy.ID, clean.ID, wisp.ID} {
			if strings.Contains(out, id) {
				t.Errorf("the refusal names %s, whose writes the mint never refuses:\n%s", id, out)
			}
		}
		if !strings.Contains(out, "2 issues") || !strings.Contains(out, "participates in history") || strings.Contains(out, "cannot tell") {
			t.Errorf("the refusal does not count the participating offenders alone or say they participate:\n%s", out)
		}
		if want := [][]string{{legacy.ID, participating.ID, gate.ID}}; !reflect.DeepEqual(st.asked, want) {
			t.Errorf("participation was asked about %v, want the offenders alone, once: %v", st.asked, want)
		}
	})

	t.Run("allows the switch when every offender is legacy", func(t *testing.T) {
		withStore(t, &participationScanStore{scanStore: scanStore{rows: rows}, participating: map[string]bool{clean.ID: true}})
		if err := enable(); err != nil {
			t.Fatalf("the switch was refused over legacy rows, which the write fence never versions: %v", err)
		}
	})

	t.Run("asks nothing of a clean store", func(t *testing.T) {
		st := &participationScanStore{scanStore: scanStore{rows: []*types.Issue{clean, wisp}}, participating: inStore}
		withStore(t, st)
		if err := enable(); err != nil {
			t.Fatalf("a store with no offender was refused: %v", err)
		}
		if len(st.asked) != 0 {
			t.Errorf("participation was read for a store with no offender: %v", st.asked)
		}
	})

	t.Run("keeps every offender, and says so, on a store that cannot tell", func(t *testing.T) {
		withStore(t, &scanStore{rows: rows})
		err := enable()
		if err == nil {
			t.Fatal("the switch was allowed although the store could not say which offenders participate")
		}
		out := err.Error()
		for _, id := range []string{legacy.ID, participating.ID, gate.ID} {
			if !strings.Contains(out, id) {
				t.Errorf("the refusal does not name %s:\n%s", id, out)
			}
		}
		if !strings.Contains(out, "3 issues") || !strings.Contains(out, "cannot tell") {
			t.Errorf("the refusal does not say it could not tell which issues participate:\n%s", out)
		}
	})

	t.Run("refuses when the participation read fails", func(t *testing.T) {
		withStore(t, &participationScanStore{scanStore: scanStore{rows: rows}, err: errors.New("unknown column 'participation_generation'")})
		err := enable()
		if err == nil || !strings.Contains(err.Error(), "cannot check the store's issues before turning versioned history on") {
			t.Fatalf("a failed participation read answered %v, want the refusal that says the check could not run", err)
		}
	})
}
