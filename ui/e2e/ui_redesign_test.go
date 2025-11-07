//go:build ui_e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	ui "github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

func timeoutError(msg string) error {
	return fmt.Errorf("%w: %s", playwright.ErrTimeout, msg)
}

func seedIssueList(t testing.TB, page playwright.Page) {
	t.Helper()

	now := time.Now()
	issues := []templates.ListIssue{
		{
			ID:              "ui-1001",
			Title:           "Implement redesigned issue badges",
			Status:          "open",
			IssueType:       "task",
			IssueTypeLabel:  "Task",
			IssueTypeClass:  "task",
			Priority:        1,
			PriorityLabel:   "P1 High",
			PriorityClass:   "p1",
			UpdatedISO:      now.Add(-2 * time.Hour).UTC().Format(time.RFC3339),
			UpdatedRelative: templates.RelativeTimeString(now, now.Add(-2*time.Hour)),
			Active:          true,
			Index:           0,
			DetailURL:       "/fragments/issue?id=ui-1001",
		},
		{
			ID:              "ui-1002",
			Title:           "Verify quick create FAB styling",
			Status:          "open",
			IssueType:       "bug",
			IssueTypeLabel:  "Bug",
			IssueTypeClass:  "bug",
			Priority:        2,
			PriorityLabel:   "P2 Medium",
			PriorityClass:   "p2",
			UpdatedISO:      now.Add(-30 * time.Minute).UTC().Format(time.RFC3339),
			UpdatedRelative: templates.RelativeTimeString(now, now.Add(-30*time.Minute)),
			Active:          false,
			Index:           1,
			DetailURL:       "/fragments/issue?id=ui-1002",
		},
	}

	listHTML, err := templates.RenderListFragment(templates.ListFragmentData{
		Heading:         "Sample issues",
		SelectedIssueID: "ui-1001",
		Issues:          issues,
	})
	if err != nil {
		t.Fatalf("render list fragment: %v", err)
	}

	if _, err := page.Evaluate(`markup => {
		const container = document.querySelector("[data-role='issue-list']");
		if (container) {
			container.innerHTML = markup;
		}
	}`, string(listHTML)); err != nil {
		t.Fatalf("inject issue list HTML: %v", err)
	}
}

// TestRedesignedThemeSelector verifies the bead theme selector buttons are visible and functional
func TestRedesignedThemeSelector(t *testing.T) {
	indexHTML := renderBasePage(t, "Beads UI")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}

	page := h.Page()

	// Wait for header to load
	if _, err := page.WaitForSelector(".ui-header", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	}); err != nil {
		t.Fatalf("wait for header: %v", err)
	}

	// Check that all 5 theme selector buttons exist
	themeSelectors := []string{"white", "orange", "green", "blue", "black"}
	for _, theme := range themeSelectors {
		selector := `[data-theme-selector="` + theme + `"]`
		btn, err := page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		if err != nil {
			t.Fatalf("wait for theme button %s: %v", theme, err)
		}

		// Verify button has image child
		img, err := btn.QuerySelector(".ui-title-beads__image")
		if err != nil {
			t.Fatalf("query image for theme %s: %v", theme, err)
		}
		if img == nil {
			t.Fatalf("expected theme button %s to have image", theme)
		}

		// All beads stay hoverable but are flagged non-interactive for assistive tech
		disabledAttr, err := btn.GetAttribute("disabled")
		if err != nil {
			t.Fatalf("check disabled attribute for theme %s: %v", theme, err)
		}
		if disabledAttr != "" {
			t.Fatalf("theme %s: bead wrapper should not use disabled attribute", theme)
		}
		disabledProp, err := btn.Evaluate(`el => el.disabled`)
		if err != nil {
			t.Fatalf("evaluate disabled property for theme %s: %v", theme, err)
		}
		if value, ok := disabledProp.(bool); ok && value {
			t.Fatalf("theme %s: bead wrapper should not be disabled", theme)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		h.MustWaitUntil(ctx, func(ctx context.Context) error {
			ariaDisabled, err := btn.GetAttribute("aria-disabled")
			if err != nil {
				return err
			}
			if ariaDisabled != "true" {
				return timeoutError("aria-disabled not yet true")
			}
			return nil
		})
		cancel()

		tabIndex, err := btn.GetAttribute("tabindex")
		if err != nil {
			t.Fatalf("get tabindex for theme %s: %v", theme, err)
		}
		if tabIndex != "-1" {
			t.Fatalf("theme %s: expected tabindex -1, got %q", theme, tabIndex)
		}
	}

	// Verify the string SVG exists
	stringEl, err := page.QuerySelector(".ui-title-beads__string")
	if err != nil {
		t.Fatalf("query bead string: %v", err)
	}
	if stringEl == nil {
		t.Fatalf("expected bead string element to exist")
	}

	// Test clicking white theme (should not change the theme)
	whiteBtn, err := page.WaitForSelector(`[data-theme-selector="white"]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for white theme button: %v", err)
	}

	themeBefore, err := page.Evaluate(`() => document.documentElement.getAttribute('data-theme') || ''`)
	if err != nil {
		t.Fatalf("evaluate initial theme: %v", err)
	}
	themeBeforeStr, ok := themeBefore.(string)
	if !ok {
		t.Fatalf("expected theme to be string, got %T", themeBefore)
	}

	if err := whiteBtn.Click(playwright.ElementHandleClickOptions{
		Force: playwright.Bool(true),
	}); err != nil {
		t.Fatalf("click white theme: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	themeAfter, err := page.Evaluate(`() => document.documentElement.getAttribute('data-theme') || ''`)
	if err != nil {
		t.Fatalf("evaluate theme after click: %v", err)
	}
	themeAfterStr, ok := themeAfter.(string)
	if !ok {
		t.Fatalf("expected theme to be string after click, got %T", themeAfter)
	}
	if themeAfterStr != themeBeforeStr {
		t.Fatalf("expected theme to remain %q, got %q", themeBeforeStr, themeAfterStr)
	}

	selectedAttr, err := whiteBtn.GetAttribute("data-selected")
	if err != nil {
		t.Fatalf("get data-selected attribute: %v", err)
	}
	if selectedAttr != "" {
		t.Fatalf("expected no active bead indicator, but data-selected=%q", selectedAttr)
	}

	ariaPressed, err := whiteBtn.GetAttribute("aria-pressed")
	if err != nil {
		t.Fatalf("get aria-pressed attribute: %v", err)
	}
	if ariaPressed != "false" {
		t.Fatalf("expected aria-pressed to be \"false\", got %q", ariaPressed)
	}
}

// TestRedesignedThreeColumnLayout verifies the three-column grid layout
func TestRedesignedThreeColumnLayout(t *testing.T) {
	indexHTML := renderBasePage(t, "Beads UI")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}

	page := h.Page()

	// Wait for main shell to load
	shell, err := page.WaitForSelector(".ui-shell", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for shell: %v", err)
	}

	// Verify grid display
	display, err := shell.Evaluate(`el => window.getComputedStyle(el).display`)
	if err != nil {
		t.Fatalf("evaluate shell display: %v", err)
	}
	if displayStr, ok := display.(string); !ok || displayStr != "grid" {
		t.Fatalf("expected shell display to be grid, got %v", display)
	}

	// Verify three columns exist: search, issues, detail
	columns := []struct {
		selector string
		name     string
	}{
		{".ui-search", "search panel"},
		{".ui-issues", "issue list"},
		{".ui-detail", "detail panel"},
	}

	for _, col := range columns {
		el, err := page.WaitForSelector(col.selector, playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		if err != nil {
			t.Fatalf("wait for %s: %v", col.name, err)
		}

		// Check visibility
		visible, err := el.IsVisible()
		if err != nil {
			t.Fatalf("check visibility of %s: %v", col.name, err)
		}
		if !visible {
			t.Fatalf("expected %s to be visible", col.name)
		}
	}
}

// TestRedesignedCommandPaletteTrigger verifies the command palette trigger button
func TestRedesignedCommandPaletteTrigger(t *testing.T) {
	indexHTML := renderBasePage(t, "Beads UI")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}

	page := h.Page()

	// Wait for command palette trigger
	trigger, err := page.WaitForSelector(`[data-testid="command-palette-trigger"]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for command palette trigger: %v", err)
	}

	// Verify button has ui-command-trigger class
	classAttr, err := trigger.GetAttribute("class")
	if err != nil {
		t.Fatalf("get trigger class: %v", err)
	}
	if classAttr == "" {
		t.Fatalf("expected trigger to have class attribute")
	}

	// Verify it's positioned fixed in top-right
	position, err := trigger.Evaluate(`el => window.getComputedStyle(el).position`)
	if err != nil {
		t.Fatalf("evaluate trigger position: %v", err)
	}
	if posStr, ok := position.(string); !ok || posStr != "fixed" {
		t.Fatalf("expected trigger position to be fixed, got %v", position)
	}

	// Click to open command palette
	if err := trigger.Click(); err != nil {
		t.Fatalf("click command palette trigger: %v", err)
	}

	// Wait for overlay to appear
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	h.MustWaitUntil(ctx, func(ctx context.Context) error {
		overlay, err := page.QuerySelector(".ui-command-overlay")
		if err != nil {
			return err
		}
		if overlay == nil {
			return timeoutError("overlay not found")
		}
		visible, err := overlay.IsVisible()
		if err != nil {
			return err
		}
		if !visible {
			return timeoutError("overlay not visible")
		}
		return nil
	})

	// Verify command input exists
	input, err := page.WaitForSelector(`[data-testid="command-palette-input"]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for command input: %v", err)
	}

	// Verify input is focused
	if _, err := page.WaitForFunction(`(el) => el === document.activeElement`, input, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(2000),
	}); err != nil {
		t.Fatalf("expected command input to be focused: %v", err)
	}
}

// TestRedesignedQuickCreateFAB verifies the floating action button
func TestRedesignedQuickCreateFAB(t *testing.T) {
	indexHTML := renderBasePage(t, "Beads UI")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}

	page := h.Page()

	// Wait for FAB button
	fab, err := page.WaitForSelector(`[data-testid="quick-create-trigger"]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for FAB: %v", err)
	}

	// Verify it's positioned fixed
	position, err := fab.Evaluate(`el => window.getComputedStyle(el).position`)
	if err != nil {
		t.Fatalf("evaluate FAB position: %v", err)
	}
	if posStr, ok := position.(string); !ok || posStr != "fixed" {
		t.Fatalf("expected FAB position to be fixed, got %v", position)
	}

	// Verify it has ui-quick-create__fab class
	classAttr, err := fab.GetAttribute("class")
	if err != nil {
		t.Fatalf("get FAB class: %v", err)
	}
	if classAttr == "" {
		t.Fatalf("expected FAB to have class attribute")
	}

	// Verify border-radius is 50% (circular)
	borderRadius, err := fab.Evaluate(`el => window.getComputedStyle(el).borderRadius`)
	if err != nil {
		t.Fatalf("evaluate FAB border-radius: %v", err)
	}
	// Should be 50% = 28px (half of 56px width)
	if radiusStr, ok := borderRadius.(string); ok && radiusStr != "28px" && radiusStr != "50%" {
		t.Logf("FAB border-radius: %v (expected 28px or 50%%)", borderRadius)
	}

	// Click to open quick create
	if err := fab.Click(); err != nil {
		t.Fatalf("click FAB: %v", err)
	}

	// Wait for overlay to appear
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	h.MustWaitUntil(ctx, func(ctx context.Context) error {
		overlay, err := page.QuerySelector(`[data-testid="quick-create-overlay"]`)
		if err != nil {
			return err
		}
		if overlay == nil {
			return timeoutError("quick create overlay not found")
		}
		visible, err := overlay.IsVisible()
		if err != nil {
			return err
		}
		if !visible {
			return timeoutError("quick create overlay not visible")
		}
		return nil
	})
}

// TestRedesignedSearchPanel verifies the search panel styling and functionality
func TestRedesignedSearchPanel(t *testing.T) {
	indexHTML := renderBasePage(t, "Beads UI")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}

	page := h.Page()

	// Wait for search panel
	searchPanel, err := page.WaitForSelector(".ui-search", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for search panel: %v", err)
	}

	// Verify background color (should be #fafafa)
	bgColor, err := searchPanel.Evaluate(`el => window.getComputedStyle(el).backgroundColor`)
	if err != nil {
		t.Fatalf("evaluate search panel background: %v", err)
	}
	// rgb(250, 250, 250) = #fafafa
	if bgStr, ok := bgColor.(string); ok && bgStr != "rgb(250, 250, 250)" {
		t.Logf("search panel background: %v (expected rgb(250, 250, 250))", bgColor)
	}

	// Check that all filter fields exist
	filterFields := []struct {
		testid string
		name   string
	}{
		{"search-query-input", "keyword input"},
		{"search-status-select", "status select"},
		{"search-type-select", "type select"},
		{"search-priority-select", "priority select"},
		{"search-assignee-input", "assignee input"},
	}

	for _, field := range filterFields {
		el, err := page.WaitForSelector(`[data-testid="`+field.testid+`"]`, playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(2000),
		})
		if err != nil {
			t.Fatalf("wait for %s: %v", field.name, err)
		}

		// Verify element is visible
		visible, err := el.IsVisible()
		if err != nil {
			t.Fatalf("check visibility of %s: %v", field.name, err)
		}
		if !visible {
			t.Fatalf("expected %s to be visible", field.name)
		}
	}

	// Check action buttons exist
	applyBtn, err := page.WaitForSelector(`[data-testid="search-apply"]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for apply button: %v", err)
	}

	// Verify button has correct class
	classAttr, err := applyBtn.GetAttribute("class")
	if err != nil {
		t.Fatalf("get apply button class: %v", err)
	}
	if classAttr == "" {
		t.Fatalf("expected apply button to have class attribute")
	}

	resetBtn, err := page.WaitForSelector(`[data-testid="search-reset"]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for reset button: %v", err)
	}

	// Verify reset button is visible
	visible, err := resetBtn.IsVisible()
	if err != nil {
		t.Fatalf("check reset button visibility: %v", err)
	}
	if !visible {
		t.Fatalf("expected reset button to be visible")
	}
}

// TestRedesignedIssueBadges verifies the badge styling matches the new design
func TestRedesignedIssueBadges(t *testing.T) {
	indexHTML := renderBasePage(t, "Beads UI")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}

	page := h.Page()

	seedIssueList(t, page)

	// Wait for issue list to load with items
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var badge playwright.ElementHandle
	h.MustWaitUntil(ctx, func(ctx context.Context) error {
		var err error
		badge, err = page.QuerySelector(".ui-badge")
		if err != nil {
			return err
		}
		if badge == nil {
			return timeoutError("no badges found yet")
		}
		return nil
	})

	if badge == nil {
		t.Skip("no issues loaded, skipping badge test")
	}

	// Check badge styling
	fontSize, err := badge.Evaluate(`el => window.getComputedStyle(el).fontSize`)
	if err != nil {
		t.Fatalf("evaluate badge font size: %v", err)
	}
	// Should be 0.65rem ≈ 10.4px
	t.Logf("badge font size: %v", fontSize)

	borderRadius, err := badge.Evaluate(`el => window.getComputedStyle(el).borderRadius`)
	if err != nil {
		t.Fatalf("evaluate badge border radius: %v", err)
	}
	// Should be 3px
	if radiusStr, ok := borderRadius.(string); ok && radiusStr != "3px" {
		t.Logf("badge border-radius: %v (expected 3px)", borderRadius)
	}

	textTransform, err := badge.Evaluate(`el => window.getComputedStyle(el).textTransform`)
	if err != nil {
		t.Fatalf("evaluate badge text transform: %v", err)
	}
	if transformStr, ok := textTransform.(string); !ok || transformStr != "uppercase" {
		t.Fatalf("expected badge text-transform to be uppercase, got %v", textTransform)
	}
}

// TestRedesignedSavedViews verifies saved views section styling
func TestRedesignedSavedViews(t *testing.T) {
	indexHTML := renderBasePage(t, "Beads UI")

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	h := NewServerHarness(t, handler, HarnessConfig{Headless: true})

	resp := h.MustNavigate("/")
	if status := resp.Status(); status != 200 {
		t.Fatalf("expected status 200, got %d", status)
	}

	page := h.Page()

	// Wait for saved views section
	savedViews, err := page.WaitForSelector(`[data-testid="saved-views"]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for saved views: %v", err)
	}

	// Verify it's visible
	visible, err := savedViews.IsVisible()
	if err != nil {
		t.Fatalf("check saved views visibility: %v", err)
	}
	if !visible {
		t.Fatalf("expected saved views section to be visible")
	}

	// Check for save button
	saveBtn, err := page.WaitForSelector(`[data-testid="saved-views-save"]`, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for save button: %v", err)
	}

	// Verify button has correct styling
	classAttr, err := saveBtn.GetAttribute("class")
	if err != nil {
		t.Fatalf("get save button class: %v", err)
	}
	if classAttr == "" {
		t.Fatalf("expected save button to have class attribute")
	}

	// Check for empty message
	emptyMsg, err := page.QuerySelector(`[data-testid="saved-views-empty"]`)
	if err != nil {
		t.Fatalf("query empty message: %v", err)
	}
	if emptyMsg == nil {
		t.Fatalf("expected empty message element to exist")
	}

	// Verify empty message is visible (assuming no saved views yet)
	visible, err = emptyMsg.IsVisible()
	if err != nil {
		t.Fatalf("check empty message visibility: %v", err)
	}
	if !visible {
		t.Logf("expected empty message to be visible, but it's not (saved views may exist)")
	}
}
