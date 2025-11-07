package static

import (
	"strings"
	"testing"
)

func TestTitleLinkHoverStylesAreSubtle(t *testing.T) {
	data, err := Files.ReadFile("styles.css")
	if err != nil {
		t.Fatalf("read styles: %v", err)
	}

	css := string(data)

	hoverBlock := cssBlock(t, css, ".ui-title__link:hover")
	if strings.Contains(hoverBlock, "background-color") {
		t.Fatalf("hover block should not include background-color: %s", hoverBlock)
	}
	if strings.Contains(hoverBlock, "box-shadow") {
		t.Fatalf("hover block should not include box-shadow: %s", hoverBlock)
	}
	if !strings.Contains(hoverBlock, "text-shadow:") {
		t.Fatalf("hover block should include subtle text-shadow, got: %s", hoverBlock)
	}
	if !strings.Contains(hoverBlock, "filter: drop-shadow") {
		t.Fatalf("hover block should include drop-shadow, got: %s", hoverBlock)
	}

	focusBlock := cssBlock(t, css, ".ui-title__link:focus-visible")
	if !strings.Contains(focusBlock, "outline: 2px solid") {
		t.Fatalf("focus block should include outline, got: %s", focusBlock)
	}
	if !strings.Contains(focusBlock, "outline-offset: 4px") {
		t.Fatalf("focus block should include outline-offset, got: %s", focusBlock)
	}
	if strings.Contains(focusBlock, "box-shadow") {
		t.Fatalf("focus block should not include box-shadow, got: %s", focusBlock)
	}
}

func cssBlock(t testing.TB, css, selector string) string {
	t.Helper()

	needle := selector + " {"
	idx := strings.Index(css, needle)
	if idx == -1 {
		t.Fatalf("selector %q not found", selector)
	}

	start := strings.Index(css[idx:], "{")
	if start == -1 {
		t.Fatalf("selector %q missing opening brace", selector)
	}
	start += idx

	depth := 0
	for i := start; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(css[start+1 : i])
			}
		}
	}

	t.Fatalf("selector %q block not closed", selector)
	return ""
}
