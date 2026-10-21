package fizzy

import "testing"

func TestDescriptionToHTML(t *testing.T) {
	got := DescriptionToHTML("\nFirst line\nsecond <line>\n\nNew paragraph & more  \n\n")
	want := "<p>First line</p><p>second &lt;line&gt;</p><p><br></p><p>New paragraph &amp; more</p>"
	if got != want {
		t.Errorf("DescriptionToHTML() = %q, want %q", got, want)
	}
	if DescriptionToHTML("  \n ") != "" {
		t.Error("an empty description must give no HTML")
	}
}

func TestDescriptionRoundTrip(t *testing.T) {
	texts := []string{
		"One line",
		"Line 1\nLine 2\n\nParagraph 2",
		"Steps:\n1. Read the diff.\n2. Commit.\n\n\nTwo blank lines before this line",
		"  indented line\n- item & <tag>\n\ttab",
		"Accents : é à ç, quotes \"x\" 'y'",
	}
	for _, text := range texts {
		html := richText(DescriptionToHTML(text))
		if got := DescriptionFromHTML(html); got != text {
			t.Errorf("round trip of %q gave %q (HTML %q)", text, got, html)
		}
	}
}

func TestDescriptionFromHTMLOfTheFizzyEditor(t *testing.T) {
	tests := map[string]string{
		// One paragraph for each line, and an empty paragraph for a blank line.
		`<div class="action-text-content"><p>a</p><p>b</p><p><br></p><p>c</p></div>`: "a\nb\n\nc",
		// Line breaks inside a paragraph, and inline formatting.
		`<p>a<br>b <strong>bold</strong> <em>it</em></p>`: "a\nb bold it",
		// Lists and headings.
		`<h1>Title</h1><ul><li>one</li><li>two</li></ul><ol><li>first</li><li>second</li></ol>`: "Title\n- one\n- two\n1. first\n2. second",
		// The format of earlier versions: <br> in a paragraph, no empty paragraph.
		`<p>Line 1<br>Line 2</p><p>Paragraph 2</p>`: "Line 1\nLine 2\nParagraph 2",
		// A code block keeps its line breaks.
		"<pre>x := 1\ny := 2</pre>": "x := 1\ny := 2",
		// Entities and non-breaking spaces.
		`<p>a&nbsp;&amp;&nbsp;b</p>`: "a & b",
		``:                           "",
	}
	for raw, want := range tests {
		if got := DescriptionFromHTML(raw); got != want {
			t.Errorf("DescriptionFromHTML(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCanonicalText(t *testing.T) {
	if got := CanonicalText("\n\na  \r\n\tb\t\n\n"); got != "a\n\tb" {
		t.Errorf("CanonicalText() = %q", got)
	}
}

func TestCardTextPrefersRichText(t *testing.T) {
	card := &Card{Description: "a\nb", DescriptionHTML: "<p>a</p><p><br></p><p>b</p>"}
	if got := cardText(card); got != "a\n\nb" {
		t.Errorf("cardText() = %q, want the blank line of the rich text", got)
	}
	if got := cardText(&Card{Description: "plain  \n"}); got != "plain" {
		t.Errorf("cardText() without rich text = %q", got)
	}
}
