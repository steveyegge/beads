package templates_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/ui/templates"
	"golang.org/x/net/html"
)

func TestRenderBasePageAppliesDefaults(t *testing.T) {
	page, err := templates.RenderBasePage(templates.BasePageData{})
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	htmlStr := string(page)
	if !strings.Contains(htmlStr, "<title>Beads</title>") {
		t.Fatalf("expected default title, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `href="/.assets/styles.css"`) {
		t.Fatalf("expected default stylesheet prefix, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `href="/.assets/images/favicon.svg"`) {
		t.Fatalf("expected default favicon path, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `src="/.assets/app.js"`) {
		t.Fatalf("expected default script prefix, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `data-live-updates="on"`) {
		t.Fatalf("expected live updates enabled, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `data-event-stream="/events"`) {
		t.Fatalf("expected default event stream URL, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `initialFilters: {`) || !strings.Contains(htmlStr, `&#34;status&#34;:&#34;open&#34;`) {
		t.Fatalf("expected initial filters serialized into shell state, got %s", htmlStr)
	}
}

func TestRenderBasePageTitleLinksToGitHub(t *testing.T) {
	page, err := templates.RenderBasePage(templates.BasePageData{})
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	htmlStr := string(page)
	if strings.Contains(htmlStr, "ui-header-link--primary") {
		t.Fatalf("expected legacy GitHub button to be removed, got %s", htmlStr)
	}

	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		t.Fatalf("parse rendered page: %v", err)
	}

	titleNode := findElementByAttr(doc, "h1", "data-testid", "ui-title")
	if titleNode == nil {
		t.Fatalf("ui title heading not found")
	}

	linkNode := findElementByAttr(titleNode, "a", "data-testid", "ui-title-link")
	if linkNode == nil {
		t.Fatalf("ui title link not found")
	}

	attrs := map[string]string{}
	for _, attr := range linkNode.Attr {
		attrs[attr.Key] = attr.Val
	}

	if got := attrs["href"]; got != "https://github.com/steveyegge/beads" {
		t.Fatalf("unexpected href %q", got)
	}
	if got := attrs["target"]; got != "_blank" {
		t.Fatalf("unexpected target %q", got)
	}
	if got := attrs["rel"]; got != "noreferrer" {
		t.Fatalf("unexpected rel %q", got)
	}
	if got := attrs["class"]; got == "" || !strings.Contains(got, "ui-title__link") {
		t.Fatalf("expected class to include ui-title__link, got %q", got)
	}

	if got := strings.TrimSpace(innerText(linkNode)); got != "Beads" {
		t.Fatalf("unexpected link text %q", got)
	}
}

func TestRenderBasePageDisablesEventStream(t *testing.T) {
	data := templates.BasePageData{
		AppTitle:             "UI Shell",
		InitialFiltersJSON:   mustDefaultFiltersJSON(t),
		EventStreamURL:       "/custom-events",
		StaticPrefix:         "/assets",
		DisableEventStream:   true,
		LiveUpdatesAvailable: false,
	}

	page, err := templates.RenderBasePage(data)
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	htmlStr := string(page)
	if !strings.Contains(htmlStr, `class="ui-body ui-body--degraded"`) {
		t.Fatalf("expected degraded body class when event stream disabled, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `data-live-updates="off"`) {
		t.Fatalf("expected live updates disabled, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `data-event-stream=""`) {
		t.Fatalf("expected empty event stream URL, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `href="/assets/styles.css"`) {
		t.Fatalf("expected custom static prefix for stylesheet, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `href="/assets/images/favicon.svg"`) {
		t.Fatalf("expected custom static prefix for favicon, got %s", htmlStr)
	}
	if !strings.Contains(htmlStr, `src="/assets/app.js"`) {
		t.Fatalf("expected custom static prefix for script, got %s", htmlStr)
	}
}

func TestRenderBasePageDefaultsToWhiteTheme(t *testing.T) {
	data := templates.BasePageData{}

	page, err := templates.RenderBasePage(data)
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		t.Fatalf("parse rendered page: %v", err)
	}

	htmlNode := findElementByTag(doc, "html")
	if htmlNode == nil {
		t.Fatalf("html element not found")
	}

	var themeAttr string
	for _, attr := range htmlNode.Attr {
		if attr.Key == "data-theme" {
			themeAttr = attr.Val
			break
		}
	}

	if themeAttr != "white" {
		t.Fatalf("expected data-theme='white', got %q", themeAttr)
	}
}

func TestRenderBasePageUsesExplicitTheme(t *testing.T) {
	data := templates.BasePageData{
		Theme: "orange",
	}

	page, err := templates.RenderBasePage(data)
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		t.Fatalf("parse rendered page: %v", err)
	}

	htmlNode := findElementByTag(doc, "html")
	if htmlNode == nil {
		t.Fatalf("html element not found")
	}

	var themeAttr string
	for _, attr := range htmlNode.Attr {
		if attr.Key == "data-theme" {
			themeAttr = attr.Val
			break
		}
	}

	if themeAttr != "orange" {
		t.Fatalf("expected data-theme='orange', got %q", themeAttr)
	}
}

func TestRenderBasePageOrangeThemeIncludesStylesheet(t *testing.T) {
	data := templates.BasePageData{
		Theme: "orange",
	}

	page, err := templates.RenderBasePage(data)
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	// Verify the page includes the stylesheet link (which contains orange theme CSS)
	htmlStr := string(page)
	if !strings.Contains(htmlStr, `href="/.assets/styles.css"`) {
		t.Fatalf("expected stylesheet link in rendered page")
	}

	// Verify data-theme attribute is set to orange
	if !strings.Contains(htmlStr, `data-theme="orange"`) {
		t.Fatalf("expected data-theme='orange' attribute in rendered page")
	}
}

func findElementByTag(node *html.Node, tag string) *html.Node {
	if node.Type == html.ElementNode && node.Data == tag {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElementByTag(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func findElementByAttr(node *html.Node, tag, attrName, attrValue string) *html.Node {
	if node.Type == html.ElementNode && node.Data == tag {
		for _, attr := range node.Attr {
			if attr.Key == attrName && attr.Val == attrValue {
				return node
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElementByAttr(child, tag, attrName, attrValue); found != nil {
			return found
		}
	}
	return nil
}

func innerText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}
