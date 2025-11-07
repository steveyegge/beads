//go:build ui_http
// +build ui_http

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uiapi "github.com/steveyegge/beads/internal/ui/api"
)

type issueSummary struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	IssueType string   `json:"issue_type"`
	Priority  int      `json:"priority"`
	Assignee  string   `json:"assignee,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}

type createResponse struct {
	Issue issueSummary `json:"issue"`
}

type listResponse struct {
	Issues     []issueSummary `json:"issues"`
	HasMore    bool           `json:"has_more"`
	NextCursor string         `json:"next_cursor"`
}

type detailIssue struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	DescriptionHTML string `json:"description_html"`
	Notes           string `json:"notes"`
	NotesHTML       string `json:"notes_html"`
	Status          string `json:"status"`
}

type detailResponse struct {
	Issue detailIssue `json:"issue"`
}

type detailUpdateResponse struct {
	Issue         issueSummary `json:"issue"`
	UpdatedFields []string     `json:"updated_fields"`
}

type labelResponse struct {
	Labels []string     `json:"labels"`
	Issue  issueSummary `json:"issue"`
}

type bulkResult struct {
	Issue   issueSummary `json:"issue"`
	Success bool         `json:"success"`
	Error   string       `json:"error"`
}

type bulkResponse struct {
	Results []bulkResult `json:"results"`
}

type searchResult struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

func TestUIHTTPWorkflow(t *testing.T) {
	workspace, dbFile := makeUITestWorkspace(t)
	stopDaemon := startTestDaemon(t, workspace, dbFile)
	t.Cleanup(stopDaemon)

	server := launchUITestServer(t, workspace, dbFile)
	session := loadUISessionSnapshot(t, server.SessionPath())

	baseURL := strings.TrimSuffix(server.BaseURL(), "/")
	if baseURL == "" {
		t.Fatal("ui server returned empty base url")
	}

	if strings.TrimSuffix(session.ListenURL, "/") != baseURL {
		t.Fatalf("session listen url mismatch: got %q want %q", session.ListenURL, baseURL)
	}

	expectedSocket := filepath.Join(filepath.Dir(dbFile), "bd.sock")
	if session.SocketPath != expectedSocket {
		t.Fatalf("session socket mismatch: got %q want %q", session.SocketPath, expectedSocket)
	}

	client := &http.Client{Timeout: 5 * time.Second}

	events, stopEvents := startEventStream(t, baseURL)
	defer stopEvents()

	const eventTimeout = 5 * time.Second

	// Create initial issues
	alphaPayload := map[string]any{
		"title":       "Alpha UI smoke",
		"description": "Alpha description body",
		"issue_type":  "feature",
		"priority":    2,
		"labels":      []string{"ui", "alpha"},
	}
	alphaRespBody, status, _ := doRequest(t, client, http.MethodPost, baseURL+"/api/issues", alphaPayload)
	if status != http.StatusCreated {
		t.Fatalf("create alpha status=%d body=%s", status, alphaRespBody)
	}
	var alphaResp createResponse
	if err := json.Unmarshal(alphaRespBody, &alphaResp); err != nil {
		t.Fatalf("decode create alpha: %v", err)
	}
	issueAlpha := alphaResp.Issue
	if issueAlpha.ID == "" {
		t.Fatal("alpha creation missing id")
	}
	expectEvent(t, events, uiapi.EventTypeCreated, issueAlpha.ID, eventTimeout)

	betaPayload := map[string]any{
		"title":       "Beta UI regression",
		"description": "Beta description",
		"issue_type":  "bug",
		"priority":    1,
	}
	betaRespBody, status, _ := doRequest(t, client, http.MethodPost, baseURL+"/api/issues", betaPayload)
	if status != http.StatusCreated {
		t.Fatalf("create beta status=%d body=%s", status, betaRespBody)
	}
	var betaResp createResponse
	if err := json.Unmarshal(betaRespBody, &betaResp); err != nil {
		t.Fatalf("decode create beta: %v", err)
	}
	issueBeta := betaResp.Issue
	if issueBeta.ID == "" {
		t.Fatal("beta creation missing id")
	}
	expectEvent(t, events, uiapi.EventTypeCreated, issueBeta.ID, eventTimeout)

	// List open issues
	listBody, status, _ := doRequest(t, client, http.MethodGet, baseURL+"/api/issues?limit=10&status=open", nil)
	if status != http.StatusOK {
		t.Fatalf("list open status=%d body=%s", status, listBody)
	}
	var list listResponse
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if findSummary(list.Issues, issueAlpha.ID) == nil || findSummary(list.Issues, issueBeta.ID) == nil {
		t.Fatalf("list missing issues: %+v", list.Issues)
	}

	// List fragment HTML
	fragmentBody, status, _ := doRequest(t, client, http.MethodGet, baseURL+"/fragments/issues?status=open&limit=10", nil)
	if status != http.StatusOK {
		t.Fatalf("list fragment status=%d body=%s", status, fragmentBody)
	}
	if !strings.Contains(string(fragmentBody), issueAlpha.ID) {
		t.Fatalf("fragment does not contain alpha id: %s", fragmentBody)
	}

	// Detail JSON
	detailURL := fmt.Sprintf("%s/api/issues/%s", baseURL, issueAlpha.ID)
	detailBody, status, _ := doRequest(t, client, http.MethodGet, detailURL, nil)
	if status != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", status, detailBody)
	}
	var detail detailResponse
	if err := json.Unmarshal(detailBody, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Issue.ID != issueAlpha.ID {
		t.Fatalf("detail id mismatch: got %s want %s", detail.Issue.ID, issueAlpha.ID)
	}

	// Detail fragment HTML
	fragmentDetailURL := fmt.Sprintf("%s/fragments/issue?id=%s", baseURL, url.QueryEscape(issueAlpha.ID))
	fragmentDetailBody, status, _ := doRequest(t, client, http.MethodGet, fragmentDetailURL, nil)
	if status != http.StatusOK {
		t.Fatalf("detail fragment status=%d body=%s", status, fragmentDetailBody)
	}
	if !strings.Contains(string(fragmentDetailBody), issueAlpha.Title) {
		t.Fatalf("detail fragment missing title: %s", fragmentDetailBody)
	}

	// Update description and notes via PATCH
	patchPayload := map[string]any{
		"description": "Updated alpha copy",
		"notes":       "Add regression coverage",
	}
	patchBody, status, _ := doRequest(t, client, http.MethodPatch, detailURL, patchPayload)
	if status != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", status, patchBody)
	}
	var patch detailUpdateResponse
	if err := json.Unmarshal(patchBody, &patch); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if len(patch.UpdatedFields) == 0 {
		t.Fatalf("expected updated fields, got %+v", patch)
	}
	expectEvent(t, events, uiapi.EventTypeUpdated, issueAlpha.ID, eventTimeout)

	// Status update to in_progress
	statusPayload := map[string]any{"status": "in_progress"}
	statusBody, sCode, _ := doRequest(t, client, http.MethodPost, fmt.Sprintf("%s/api/issues/%s/status", baseURL, issueAlpha.ID), statusPayload)
	if sCode != http.StatusOK {
		t.Fatalf("status update code=%d body=%s", sCode, statusBody)
	}
	expectEvent(t, events, uiapi.EventTypeUpdated, issueAlpha.ID, eventTimeout)

	// Add label
	labelBody, sCode, _ := doRequest(t, client, http.MethodPost, fmt.Sprintf("%s/api/issues/%s/labels", baseURL, issueAlpha.ID), map[string]string{"label": "frontend"})
	if sCode != http.StatusOK {
		t.Fatalf("label add code=%d body=%s", sCode, labelBody)
	}
	var labelsResp labelResponse
	if err := json.Unmarshal(labelBody, &labelsResp); err != nil {
		t.Fatalf("decode label add: %v", err)
	}
	if !containsString(labelsResp.Labels, "frontend") {
		t.Fatalf("label not applied: %+v", labelsResp.Labels)
	}

	// Remove label
	removeBody, sCode, _ := doRequest(t, client, http.MethodDelete, fmt.Sprintf("%s/api/issues/%s/labels", baseURL, issueAlpha.ID), map[string]string{"label": "frontend"})
	if sCode != http.StatusOK {
		t.Fatalf("label remove code=%d body=%s", sCode, removeBody)
	}
	var removeResp labelResponse
	if err := json.Unmarshal(removeBody, &removeResp); err != nil {
		t.Fatalf("decode label remove: %v", err)
	}
	if containsString(removeResp.Labels, "frontend") {
		t.Fatalf("label still present after removal: %+v", removeResp.Labels)
	}

	// Create gamma issue for search coverage
	gammaPayload := map[string]any{
		"title":       "Gamma docs polish",
		"description": "Tidy up docs and UI copy",
		"issue_type":  "task",
		"priority":    3,
	}
	gammaBody, status, _ := doRequest(t, client, http.MethodPost, baseURL+"/api/issues", gammaPayload)
	if status != http.StatusCreated {
		t.Fatalf("create gamma status=%d body=%s", status, gammaBody)
	}
	var gammaResp createResponse
	if err := json.Unmarshal(gammaBody, &gammaResp); err != nil {
		t.Fatalf("decode create gamma: %v", err)
	}
	issueGamma := gammaResp.Issue
	expectEvent(t, events, uiapi.EventTypeCreated, issueGamma.ID, eventTimeout)

	// Bulk update alpha + beta
	bulkPayload := map[string]any{
		"ids": []string{issueAlpha.ID, issueBeta.ID},
		"action": map[string]any{
			"status":   "blocked",
			"priority": 0,
		},
	}
	bulkBody, status, _ := doRequest(t, client, http.MethodPost, baseURL+"/api/issues/bulk", bulkPayload)
	if status != http.StatusOK {
		t.Fatalf("bulk status=%d body=%s", status, bulkBody)
	}
	var bulkResp bulkResponse
	if err := json.Unmarshal(bulkBody, &bulkResp); err != nil {
		t.Fatalf("decode bulk: %v", err)
	}
	if len(bulkResp.Results) < 2 {
		t.Fatalf("bulk results missing entries: %+v", bulkResp.Results)
	}
	expectEvent(t, events, uiapi.EventTypeUpdated, issueAlpha.ID, eventTimeout)
	expectEvent(t, events, uiapi.EventTypeUpdated, issueBeta.ID, eventTimeout)

	// Search endpoint
	searchBody, status, _ := doRequest(t, client, http.MethodGet, baseURL+"/api/search?q=UI&limit=5", nil)
	if status != http.StatusOK {
		t.Fatalf("search status=%d body=%s", status, searchBody)
	}
	var searchResp searchResponse
	if err := json.Unmarshal(searchBody, &searchResp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchResp.Results) == 0 {
		t.Fatal("search returned no results")
	}

	// Theme endpoint
	_, status, headers := doRequest(t, client, http.MethodPost, baseURL+"/api/theme", map[string]string{"theme": "blue"})
	if status != http.StatusOK {
		t.Fatalf("theme status=%d", status)
	}
	cookies := headers.Values("Set-Cookie")
	if !cookieContains(cookies, "beads-theme=blue") {
		t.Fatalf("theme cookie missing, headers=%v", cookies)
	}

	// Delete beta issue
	deleteURL := fmt.Sprintf("%s/api/issues/%s?confirm=%s", baseURL, issueBeta.ID, url.QueryEscape(issueBeta.ID))
	delBody, status, _ := doRequest(t, client, http.MethodDelete, deleteURL, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", status, delBody)
	}
	expectEvent(t, events, uiapi.EventTypeDeleted, issueBeta.ID, eventTimeout)

	// Close alpha issue
	closePayload := map[string]any{"status": "closed"}
	closeBody, status, _ := doRequest(t, client, http.MethodPost, fmt.Sprintf("%s/api/issues/%s/status", baseURL, issueAlpha.ID), closePayload)
	if status != http.StatusOK {
		t.Fatalf("close status=%d body=%s", status, closeBody)
	}
	expectEvent(t, events, uiapi.EventTypeUpdated, issueAlpha.ID, eventTimeout)

	// Closed queue list
	closedBody, status, _ := doRequest(t, client, http.MethodGet, baseURL+"/api/issues?queue=closed&limit=10", nil)
	if status != http.StatusOK {
		t.Fatalf("closed queue status=%d body=%s", status, closedBody)
	}
	var closed listResponse
	if err := json.Unmarshal(closedBody, &closed); err != nil {
		t.Fatalf("decode closed list: %v", err)
	}
	if findSummary(closed.Issues, issueAlpha.ID) == nil {
		t.Fatalf("closed list missing alpha: %+v", closed.Issues)
	}

	// Closed fragment append mode
	appendBody, status, _ := doRequest(t, client, http.MethodGet, baseURL+"/fragments/issues?status=closed&limit=5&append=1", nil)
	if status != http.StatusOK {
		t.Fatalf("closed append status=%d body=%s", status, appendBody)
	}
	if !strings.Contains(string(appendBody), issueAlpha.ID) {
		t.Fatalf("closed append missing alpha id: %s", appendBody)
	}

	// Verify deleted issue missing from open list
	openAfterDeleteBody, status, _ := doRequest(t, client, http.MethodGet, baseURL+"/api/issues?limit=10&status=open", nil)
	if status != http.StatusOK {
		t.Fatalf("list open after delete status=%d body=%s", status, openAfterDeleteBody)
	}
	var openAfter listResponse
	if err := json.Unmarshal(openAfterDeleteBody, &openAfter); err != nil {
		t.Fatalf("decode open after delete: %v", err)
	}
	if findSummary(openAfter.Issues, issueBeta.ID) != nil {
		t.Fatalf("deleted issue still in list: %+v", openAfter.Issues)
	}
}

func doRequest(t testing.TB, client *http.Client, method, endpoint string, payload any) ([]byte, int, http.Header) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, endpoint, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return data, resp.StatusCode, resp.Header.Clone()
}

type sseMessage struct {
	Event string
	Data  []byte
}

func startEventStream(t testing.TB, baseURL string) (<-chan uiapi.IssueEvent, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/events", nil)
	if err != nil {
		t.Fatalf("new events request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}

	events := make(chan uiapi.IssueEvent, 32)
	done := make(chan struct{})

	go func() {
		defer close(events)
		defer close(done)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		for {
			msg, err := readSSEMessage(reader)
			if err != nil {
				return
			}
			if msg.Event == "" || msg.Event == "heartbeat" {
				continue
			}
			var evt uiapi.IssueEvent
			if err := json.Unmarshal(msg.Data, &evt); err != nil {
				continue
			}
			events <- evt
		}
	}()

	stop := func() {
		cancel()
		_, _ = io.Copy(io.Discard, resp.Body)
		<-done
	}

	return events, stop
}

func readSSEMessage(reader *bufio.Reader) (sseMessage, error) {
	var msg sseMessage

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return msg, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if msg.Event == "" && len(msg.Data) == 0 {
				continue
			}
			return msg, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			msg.Event = strings.TrimSpace(line[6:])
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if len(msg.Data) > 0 {
				msg.Data = append(msg.Data, '\n')
			}
			msg.Data = append(msg.Data, strings.TrimSpace(line[5:])...)
		}
	}
}

func expectEvent(t testing.TB, events <-chan uiapi.IssueEvent, eventType uiapi.EventType, issueID string, timeout time.Duration) uiapi.IssueEvent {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case evt, ok := <-events:
			if !ok {
				t.Fatalf("event stream closed while waiting for %s on %s", eventType, issueID)
			}
			if evt.Type == eventType && evt.Issue.ID == issueID {
				return evt
			}
		case <-timer.C:
			t.Fatalf("timeout waiting for %s event for %s", eventType, issueID)
		}
	}
}

func findSummary(list []issueSummary, id string) *issueSummary {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func cookieContains(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
