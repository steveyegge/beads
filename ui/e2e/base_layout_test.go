//go:build ui_e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	playwright "github.com/playwright-community/playwright-go"
	ui "github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/ui/static"
)

func TestBaseLayoutStructure(t *testing.T) {
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

	selectors := []string{
		`[data-role="search-panel"]`,
		`[data-role="issue-list"]`,
		`[data-role="issue-detail"]`,
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h.MustWaitUntil(waitCtx, func(ctx context.Context) error {
		for _, selector := range selectors {
			if _, err := page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{
				Timeout: playwright.Float(1000),
			}); err != nil {
				return err
			}
		}
		return nil
	})

	role, err := page.Evaluate(`() => {
		const el = document.activeElement;
		return el ? el.getAttribute("data-role") : null;
	}`)
	if err != nil {
		t.Fatalf("evaluate active element: %v", err)
	}
	if roleStr, _ := role.(string); roleStr != "issue-list" {
		t.Fatalf("expected issue list to receive initial focus, got %v", role)
	}
}

func TestHeaderTitleLinksToGitHub(t *testing.T) {
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

	link, err := page.WaitForSelector("[data-testid='ui-title-link']", playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(2000),
	})
	if err != nil {
		t.Fatalf("wait for title link: %v", err)
	}

	href, err := link.GetAttribute("href")
	if err != nil {
		t.Fatalf("get title link href: %v", err)
	}
	if href != "https://github.com/steveyegge/beads" {
		t.Fatalf("expected title link href to GitHub, got %q", href)
	}

	classAttr, err := link.GetAttribute("class")
	if err != nil {
		t.Fatalf("get title link class: %v", err)
	}
	if classAttr == "" || !strings.Contains(classAttr, "ui-title__link") {
		t.Fatalf("expected title link to include ui-title__link class, got %q", classAttr)
	}

	if btn, err := page.QuerySelector(".ui-header-link--primary"); err != nil {
		t.Fatalf("query old GitHub button: %v", err)
	} else if btn != nil {
		t.Fatalf("expected legacy GitHub button to be removed")
	}

	target, err := link.GetAttribute("target")
	if err != nil {
		t.Fatalf("get title link target: %v", err)
	}
	if target != "_blank" {
		t.Fatalf("expected title link target to be _blank, got %q", target)
	}

	rel, err := link.GetAttribute("rel")
	if err != nil {
		t.Fatalf("get title link rel: %v", err)
	}
	if rel != "noreferrer" {
		t.Fatalf("expected title link rel to be noreferrer, got %q", rel)
	}
}
