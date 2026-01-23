package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/memory"
	"github.com/steveyegge/beads/internal/types"
)

func setupTestServer(t *testing.T) (*Server, *memory.MemoryStorage, []byte) {
	t.Helper()

	store := memory.New("")
	secret := []byte("test-secret")

	server := NewServer(ServerConfig{
		Store:      store,
		Secret:     secret,
		StrictMode: false,
	})

	return server, store, secret
}

func createTestDecision(t *testing.T, store *memory.MemoryStorage, id string) (*types.Issue, *types.DecisionPoint) {
	t.Helper()
	ctx := context.Background()

	// Create decision gate issue
	issue := &types.Issue{
		ID:        id,
		Title:     "Test Decision",
		IssueType: types.TypeGate,
		AwaitType: "decision",
		Status:    types.StatusOpen,
		Priority:  2,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	// Create decision point data
	dp := &types.DecisionPoint{
		IssueID:       id,
		Prompt:        "Which option?",
		Options:       `[{"id":"a","short":"A","label":"Option A"},{"id":"b","short":"B","label":"Option B"}]`,
		DefaultOption: "a",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
	}
	if err := store.CreateDecisionPoint(ctx, dp); err != nil {
		t.Fatalf("failed to save decision point: %v", err)
	}

	return issue, dp
}

func TestHandleDecisionResponse_Success(t *testing.T) {
	server, store, secret := setupTestServer(t)
	decisionID := "gt-test.decision-1"
	createTestDecision(t, store, decisionID)

	// Generate valid token
	token, err := GenerateResponseToken(decisionID, time.Now().Add(time.Hour), "", secret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Create request
	reqBody := RespondRequest{
		Selected:   "a",
		Text:       "Additional notes",
		Respondent: "user@example.com",
		AuthToken:  token,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/decisions/"+decisionID+"/respond", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp RespondResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if !resp.Success {
		t.Errorf("Success = false, want true; Error: %s", resp.Error)
	}
	if resp.DecisionID != decisionID {
		t.Errorf("DecisionID = %q, want %q", resp.DecisionID, decisionID)
	}
	if resp.Selected != "a" {
		t.Errorf("Selected = %q, want %q", resp.Selected, "a")
	}

	// Verify decision was updated
	ctx := context.Background()
	dp, _ := store.GetDecisionPoint(ctx, decisionID)
	if dp.SelectedOption != "a" {
		t.Errorf("SelectedOption = %q, want %q", dp.SelectedOption, "a")
	}
	if dp.RespondedAt == nil {
		t.Error("RespondedAt should be set")
	}

	// Verify issue was closed
	issue, _ := store.GetIssue(ctx, decisionID)
	if issue.Status != types.StatusClosed {
		t.Errorf("Issue status = %q, want %q", issue.Status, types.StatusClosed)
	}
}

func TestHandleDecisionResponse_InvalidToken(t *testing.T) {
	server, store, _ := setupTestServer(t)
	decisionID := "gt-test.decision-1"
	createTestDecision(t, store, decisionID)

	// Create request with invalid token
	reqBody := RespondRequest{
		Selected:   "a",
		Respondent: "user@example.com",
		AuthToken:  "invalid-token",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/decisions/"+decisionID+"/respond", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleDecisionResponse_WrongDecision(t *testing.T) {
	server, store, secret := setupTestServer(t)
	decisionID := "gt-test.decision-1"
	createTestDecision(t, store, decisionID)

	// Generate token for different decision
	token, _ := GenerateResponseToken("gt-other.decision-1", time.Now().Add(time.Hour), "", secret)

	reqBody := RespondRequest{
		Selected:  "a",
		AuthToken: token,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/decisions/"+decisionID+"/respond", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleDecisionResponse_AlreadyResponded(t *testing.T) {
	server, store, secret := setupTestServer(t)
	decisionID := "gt-test.decision-1"
	_, dp := createTestDecision(t, store, decisionID)

	// Mark as already responded
	now := time.Now()
	dp.RespondedAt = &now
	dp.RespondedBy = "other@example.com"
	_ = store.UpdateDecisionPoint(context.Background(), dp)

	// Generate valid token
	token, _ := GenerateResponseToken(decisionID, time.Now().Add(time.Hour), "", secret)

	reqBody := RespondRequest{
		Selected:  "a",
		AuthToken: token,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/decisions/"+decisionID+"/respond", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleDecisionResponse_InvalidOption(t *testing.T) {
	server, store, secret := setupTestServer(t)
	decisionID := "gt-test.decision-1"
	createTestDecision(t, store, decisionID)

	// Generate valid token
	token, _ := GenerateResponseToken(decisionID, time.Now().Add(time.Hour), "", secret)

	reqBody := RespondRequest{
		Selected:  "invalid-option",
		AuthToken: token,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/decisions/"+decisionID+"/respond", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDecisionResponse_TextOnly(t *testing.T) {
	server, store, secret := setupTestServer(t)
	decisionID := "gt-test.decision-1"
	createTestDecision(t, store, decisionID)

	// Generate valid token
	token, _ := GenerateResponseToken(decisionID, time.Now().Add(time.Hour), "", secret)

	// Request with only text (no selection)
	reqBody := RespondRequest{
		Text:       "I prefer a different approach",
		Respondent: "user@example.com",
		AuthToken:  token,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/decisions/"+decisionID+"/respond", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify decision was updated but gate NOT closed (text only doesn't close)
	ctx := context.Background()
	dp, _ := store.GetDecisionPoint(ctx, decisionID)
	if dp.ResponseText != "I prefer a different approach" {
		t.Errorf("ResponseText = %q, want %q", dp.ResponseText, "I prefer a different approach")
	}

	issue, _ := store.GetIssue(ctx, decisionID)
	if issue.Status == types.StatusClosed {
		t.Error("Issue should NOT be closed for text-only response")
	}
}

func TestHandleDecisionResponse_StrictRespondent(t *testing.T) {
	store := memory.New("")
	secret := []byte("test-secret")

	server := NewServer(ServerConfig{
		Store:      store,
		Secret:     secret,
		StrictMode: true, // Enable strict mode
	})

	decisionID := "gt-test.decision-1"
	createTestDecision(t, store, decisionID)

	// Generate token for specific respondent
	token, _ := GenerateResponseToken(decisionID, time.Now().Add(time.Hour), "expected@example.com", secret)

	// Request from different respondent
	reqBody := RespondRequest{
		Selected:   "a",
		Respondent: "other@example.com",
		AuthToken:  token,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/decisions/"+decisionID+"/respond", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleHealth(t *testing.T) {
	server, _, _ := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want %q", resp["status"], "ok")
	}
}
