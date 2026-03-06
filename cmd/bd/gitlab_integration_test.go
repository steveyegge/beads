//go:build cgo && integration
// +build cgo,integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/gitlab"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

type gitLabIssuePayload struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	StateEvent  string   `json:"state_event,omitempty"`
}

type gitLabSyncTestServer struct {
	t      *testing.T
	server *httptest.Server

	mu             sync.Mutex
	issues         map[int]gitlab.Issue
	nextID         int
	nextIID        int
	listQueries    []url.Values
	createRequests []gitLabIssuePayload
	updateRequests []gitLabIssuePayload
}

func newGitLabSyncTestServer(t *testing.T, issues ...gitlab.Issue) *gitLabSyncTestServer {
	t.Helper()

	s := &gitLabSyncTestServer{
		t:       t,
		issues:  make(map[int]gitlab.Issue),
		nextID:  1000,
		nextIID: 1,
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	for _, issue := range issues {
		s.addIssue(issue)
	}
	t.Cleanup(s.server.Close)
	return s
}

func (s *gitLabSyncTestServer) URL() string {
	return s.server.URL
}

func (s *gitLabSyncTestServer) issueURL(iid int) string {
	return "https://gitlab.test/project/-/issues/" + strconv.Itoa(iid)
}

func (s *gitLabSyncTestServer) addIssue(issue gitlab.Issue) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if issue.IID == 0 {
		issue.IID = s.nextIID
	}
	if issue.ID == 0 {
		issue.ID = s.nextID
	}
	if issue.ProjectID == 0 {
		issue.ProjectID = 123
	}
	if issue.CreatedAt == nil {
		createdAt := now
		issue.CreatedAt = &createdAt
	}
	if issue.UpdatedAt == nil {
		updatedAt := issue.CreatedAt.UTC()
		issue.UpdatedAt = &updatedAt
	}
	if issue.State == "" {
		issue.State = "opened"
	}
	if issue.WebURL == "" {
		issue.WebURL = s.issueURL(issue.IID)
	}
	s.issues[issue.IID] = issue
	if issue.ID >= s.nextID {
		s.nextID = issue.ID + 1
	}
	if issue.IID >= s.nextIID {
		s.nextIID = issue.IID + 1
	}
}

func (s *gitLabSyncTestServer) createRequestsSnapshot() []gitLabIssuePayload {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]gitLabIssuePayload, len(s.createRequests))
	copy(out, s.createRequests)
	return out
}

func (s *gitLabSyncTestServer) updateRequestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.updateRequests)
}

func (s *gitLabSyncTestServer) listQueriesSnapshot() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]url.Values, len(s.listQueries))
	for i, values := range s.listQueries {
		out[i] = cloneValues(values)
	}
	return out
}

func (s *gitLabSyncTestServer) handle(w http.ResponseWriter, r *http.Request) {
	s.t.Helper()

	if token := r.Header.Get("PRIVATE-TOKEN"); token != "test-token" {
		s.t.Fatalf("PRIVATE-TOKEN = %q, want %q", token, "test-token")
	}

	w.Header().Set("Content-Type", "application/json")

	const issuesPath = "/api/v4/projects/123/issues"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == issuesPath:
		s.handleListIssues(w, r)
	case r.Method == http.MethodPost && r.URL.Path == issuesPath:
		s.handleCreateIssue(w, r)
	case strings.HasPrefix(r.URL.Path, issuesPath+"/"):
		s.handleIssue(w, r, strings.TrimPrefix(r.URL.Path, issuesPath+"/"))
	default:
		http.NotFound(w, r)
	}
}

func (s *gitLabSyncTestServer) handleListIssues(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.listQueries = append(s.listQueries, cloneValues(r.URL.Query()))

	var updatedAfter time.Time
	var filterSince bool
	if raw := r.URL.Query().Get("updated_after"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			s.t.Fatalf("invalid updated_after %q: %v", raw, err)
		}
		updatedAfter = parsed
		filterSince = true
	}

	state := r.URL.Query().Get("state")
	issues := make([]gitlab.Issue, 0, len(s.issues))
	for _, issue := range s.issues {
		if filterSince && (issue.UpdatedAt == nil || !issue.UpdatedAt.After(updatedAfter)) {
			continue
		}
		if state != "" && state != "all" && issue.State != state {
			continue
		}
		issues = append(issues, issue)
	}

	sort.Slice(issues, func(i, j int) bool { return issues[i].IID < issues[j].IID })
	if err := json.NewEncoder(w).Encode(issues); err != nil {
		s.t.Fatalf("encode list response: %v", err)
	}
}

func (s *gitLabSyncTestServer) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var payload gitLabIssuePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.t.Fatalf("decode create payload: %v", err)
	}
	s.createRequests = append(s.createRequests, payload)

	now := time.Now().UTC()
	createdAt := now
	updatedAt := now
	issue := gitlab.Issue{
		ID:          s.nextID,
		IID:         s.nextIID,
		ProjectID:   123,
		Title:       payload.Title,
		Description: payload.Description,
		State:       "opened",
		Labels:      append([]string(nil), payload.Labels...),
		CreatedAt:   &createdAt,
		UpdatedAt:   &updatedAt,
		WebURL:      s.issueURL(s.nextIID),
	}
	s.issues[issue.IID] = issue
	s.nextID++
	s.nextIID++

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(issue); err != nil {
		s.t.Fatalf("encode create response: %v", err)
	}
}

func (s *gitLabSyncTestServer) handleIssue(w http.ResponseWriter, r *http.Request, iidText string) {
	iid, err := strconv.Atoi(iidText)
	if err != nil {
		s.t.Fatalf("parse IID %q: %v", iidText, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	issue, ok := s.issues[iid]
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if err := json.NewEncoder(w).Encode(issue); err != nil {
			s.t.Fatalf("encode get response: %v", err)
		}
	case http.MethodPut:
		var payload gitLabIssuePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.t.Fatalf("decode update payload: %v", err)
		}
		s.updateRequests = append(s.updateRequests, payload)

		if payload.Title != "" {
			issue.Title = payload.Title
		}
		if payload.Description != "" {
			issue.Description = payload.Description
		}
		if payload.Labels != nil {
			issue.Labels = append([]string(nil), payload.Labels...)
		}
		if payload.StateEvent == "close" {
			issue.State = "closed"
		}
		updatedAt := time.Now().UTC()
		issue.UpdatedAt = &updatedAt
		s.issues[iid] = issue

		if err := json.NewEncoder(w).Encode(issue); err != nil {
			s.t.Fatalf("encode update response: %v", err)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		copied := make([]string, len(items))
		copy(copied, items)
		cloned[key] = copied
	}
	return cloned
}

func setupGitLabSyncIntegration(t *testing.T, serverURL string) (context.Context, *dolt.DoltStore) {
	t.Helper()

	ensureCleanGlobalState(t)
	saveAndRestoreGlobals(t)
	saveAndRestoreGitLabSyncFlags(t)

	oldActor := actor
	oldReadonly := readonlyMode
	actor = "test-actor"
	readonlyMode = false
	t.Cleanup(func() {
		actor = oldActor
		readonlyMode = oldReadonly
	})

	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	testStore := newTestStore(t, testDB)

	ctx := context.Background()
	store = testStore
	storeActive = true
	dbPath = testDB

	for key, value := range map[string]string{
		"gitlab.url":        serverURL,
		"gitlab.token":      "test-token",
		"gitlab.project_id": "123",
	} {
		if err := testStore.SetConfig(ctx, key, value); err != nil {
			t.Fatalf("SetConfig(%s): %v", key, err)
		}
	}

	return ctx, testStore
}

func saveAndRestoreGitLabSyncFlags(t *testing.T) {
	t.Helper()

	oldDryRun := gitlabSyncDryRun
	oldPullOnly := gitlabSyncPullOnly
	oldPushOnly := gitlabSyncPushOnly
	oldPreferLocal := gitlabPreferLocal
	oldPreferGitLab := gitlabPreferGitLab
	oldPreferNewer := gitlabPreferNewer

	gitlabSyncDryRun = false
	gitlabSyncPullOnly = false
	gitlabSyncPushOnly = false
	gitlabPreferLocal = false
	gitlabPreferGitLab = false
	gitlabPreferNewer = false

	t.Cleanup(func() {
		gitlabSyncDryRun = oldDryRun
		gitlabSyncPullOnly = oldPullOnly
		gitlabSyncPushOnly = oldPushOnly
		gitlabPreferLocal = oldPreferLocal
		gitlabPreferGitLab = oldPreferGitLab
		gitlabPreferNewer = oldPreferNewer
	})
}

func runGitLabSyncForTest(t *testing.T) string {
	t.Helper()

	var out bytes.Buffer
	cmd := &cobra.Command{Use: "sync"}
	cmd.SetOut(&out)

	if err := runGitLabSync(cmd, nil); err != nil {
		t.Fatalf("runGitLabSync() error = %v", err)
	}

	return out.String()
}

func TestGitLabSyncCommandBidirectional(t *testing.T) {
	remoteCreatedAt := time.Date(2026, time.January, 2, 10, 0, 0, 0, time.UTC)
	remoteUpdatedAt := remoteCreatedAt.Add(2 * time.Hour)
	server := newGitLabSyncTestServer(t, gitlab.Issue{
		ID:          101,
		IID:         1,
		ProjectID:   123,
		Title:       "Remote GitLab issue",
		Description: "Imported from GitLab",
		State:       "opened",
		Labels:      []string{"type::bug", "priority::high", "backend"},
		CreatedAt:   &remoteCreatedAt,
		UpdatedAt:   &remoteUpdatedAt,
	})

	ctx, testStore := setupGitLabSyncIntegration(t, server.URL())

	local := &types.Issue{
		Title:       "Local issue to push",
		Description: "Created locally",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := testStore.CreateIssue(ctx, local, actor); err != nil {
		t.Fatalf("CreateIssue(local): %v", err)
	}

	output := runGitLabSyncForTest(t)
	if !strings.Contains(output, "Pulled 1 issues") {
		t.Fatalf("sync output missing pull summary: %q", output)
	}
	if !strings.Contains(output, "✓ Pushed ") {
		t.Fatalf("sync output missing push summary: %q", output)
	}

	imported, err := testStore.GetIssueByExternalRef(ctx, server.issueURL(1))
	if err != nil {
		t.Fatalf("GetIssueByExternalRef(remote): %v", err)
	}
	if imported == nil {
		t.Fatal("expected pulled GitLab issue to exist locally")
	}
	if imported.SourceSystem != "gitlab:123:1" {
		t.Errorf("imported.SourceSystem = %q, want %q", imported.SourceSystem, "gitlab:123:1")
	}
	if imported.Title != "Remote GitLab issue" {
		t.Errorf("imported.Title = %q, want %q", imported.Title, "Remote GitLab issue")
	}
	if imported.Priority != 1 {
		t.Errorf("imported.Priority = %d, want 1", imported.Priority)
	}
	if imported.IssueType != types.TypeBug {
		t.Errorf("imported.IssueType = %q, want %q", imported.IssueType, types.TypeBug)
	}

	pushed, err := testStore.GetIssue(ctx, local.ID)
	if err != nil {
		t.Fatalf("GetIssue(local): %v", err)
	}
	if pushed == nil || pushed.ExternalRef == nil {
		t.Fatal("expected pushed local issue to gain an external_ref")
	}
	if got := *pushed.ExternalRef; got != server.issueURL(2) {
		t.Errorf("pushed.ExternalRef = %q, want %q", got, server.issueURL(2))
	}

	createRequests := server.createRequestsSnapshot()
	if len(createRequests) != 1 {
		t.Fatalf("create request count = %d, want 1", len(createRequests))
	}
	if createRequests[0].Title != local.Title {
		t.Errorf("createRequests[0].Title = %q, want %q", createRequests[0].Title, local.Title)
	}
	if createRequests[0].Description != local.Description {
		t.Errorf("createRequests[0].Description = %q, want %q", createRequests[0].Description, local.Description)
	}
	if !hasString(createRequests[0].Labels, "type::task") {
		t.Errorf("createRequests[0].Labels missing %q: %v", "type::task", createRequests[0].Labels)
	}
	if !hasString(createRequests[0].Labels, "priority::medium") {
		t.Errorf("createRequests[0].Labels missing %q: %v", "priority::medium", createRequests[0].Labels)
	}

	if server.updateRequestCount() != 0 {
		t.Errorf("unexpected GitLab update calls: got %d, want 0", server.updateRequestCount())
	}

	issues, err := testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues(): %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("len(issues) = %d, want 2", len(issues))
	}

	lastSync, err := testStore.GetConfig(ctx, "gitlab.last_sync")
	if err != nil {
		t.Fatalf("GetConfig(gitlab.last_sync): %v", err)
	}
	if lastSync == "" {
		t.Fatal("expected gitlab.last_sync to be recorded")
	}
}

func TestGitLabSyncCommandIncrementalPullOnly(t *testing.T) {
	initialCreatedAt := time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)
	initialUpdatedAt := initialCreatedAt.Add(30 * time.Minute)
	server := newGitLabSyncTestServer(t, gitlab.Issue{
		ID:          201,
		IID:         1,
		ProjectID:   123,
		Title:       "Existing remote issue",
		Description: "Available before first sync",
		State:       "opened",
		CreatedAt:   &initialCreatedAt,
		UpdatedAt:   &initialUpdatedAt,
	})

	ctx, testStore := setupGitLabSyncIntegration(t, server.URL())
	gitlabSyncPullOnly = true

	firstOutput := runGitLabSyncForTest(t)
	if !strings.Contains(firstOutput, "Pulled 1 issues") {
		t.Fatalf("first sync output missing pull summary: %q", firstOutput)
	}

	firstQueries := server.listQueriesSnapshot()
	if len(firstQueries) != 1 {
		t.Fatalf("list query count after first sync = %d, want 1", len(firstQueries))
	}
	if got := firstQueries[0].Get("updated_after"); got != "" {
		t.Errorf("first sync updated_after = %q, want empty", got)
	}

	lastSync, err := testStore.GetConfig(ctx, "gitlab.last_sync")
	if err != nil {
		t.Fatalf("GetConfig(gitlab.last_sync): %v", err)
	}
	lastSyncTime, err := time.Parse(time.RFC3339, lastSync)
	if err != nil {
		t.Fatalf("Parse(last_sync): %v", err)
	}

	secondCreatedAt := lastSyncTime.Add(time.Minute)
	secondUpdatedAt := secondCreatedAt.Add(time.Minute)
	server.addIssue(gitlab.Issue{
		ID:          202,
		IID:         2,
		ProjectID:   123,
		Title:       "New remote issue",
		Description: "Added after first sync",
		State:       "opened",
		CreatedAt:   &secondCreatedAt,
		UpdatedAt:   &secondUpdatedAt,
	})

	secondOutput := runGitLabSyncForTest(t)
	if !strings.Contains(secondOutput, "Pulled 1 issues") {
		t.Fatalf("second sync output missing pull summary: %q", secondOutput)
	}

	queries := server.listQueriesSnapshot()
	if len(queries) != 2 {
		t.Fatalf("list query count after second sync = %d, want 2", len(queries))
	}
	if got := queries[1].Get("updated_after"); got == "" {
		t.Fatal("expected incremental sync request to include updated_after")
	}

	imported, err := testStore.GetIssueByExternalRef(ctx, server.issueURL(2))
	if err != nil {
		t.Fatalf("GetIssueByExternalRef(new remote): %v", err)
	}
	if imported == nil {
		t.Fatal("expected incremental sync to import the new remote issue")
	}

	issues, err := testStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("SearchIssues(): %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("len(issues) = %d, want 2", len(issues))
	}

	if createCount := len(server.createRequestsSnapshot()); createCount != 0 {
		t.Errorf("create request count = %d, want 0", createCount)
	}
}

func hasString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
