package fizzy

import (
	"html"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

// The editor of Fizzy (Lexxy) keeps one paragraph for each line of text, and an
// empty paragraph (<p><br></p>) for a blank line. Its stylesheet puts no margin
// between two paragraphs. A description must use the same structure, or Fizzy
// shows it without its line breaks.

// DescriptionToHTML converts a plain text description to the rich text of a card:
// one paragraph for each line, and an empty paragraph for each blank line.
func DescriptionToHTML(text string) string {
	text = CanonicalText(text)
	if text == "" {
		return ""
	}
	var builder strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			builder.WriteString("<p><br></p>")
			continue
		}
		builder.WriteString("<p>" + html.EscapeString(line) + "</p>")
	}
	return builder.String()
}

// DescriptionFromHTML converts the rich text of a card to plain text. A paragraph,
// a heading, a list item or a line break starts a new line. An empty paragraph
// gives a blank line. A list item starts with "- " or with its number.
func DescriptionFromHTML(raw string) string {
	type block struct {
		tag         string
		hasChildren bool
		endsWithBR  bool
		items       int // number of the last item of an ordered list
	}
	var (
		lines   []string
		current strings.Builder
		stack   []*block
	)
	flush := func() {
		lines = append(lines, strings.TrimRight(current.String(), " \t"))
		current.Reset()
	}
	top := func() *block {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}
	inPre := func() bool {
		for _, open := range stack {
			if open.tag == "pre" {
				return true
			}
		}
		return false
	}

	tokenizer := xhtml.NewTokenizer(strings.NewReader(raw))
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case xhtml.ErrorToken:
			if current.Len() > 0 {
				flush()
			}
			return CanonicalText(strings.Join(lines, "\n"))

		case xhtml.TextToken:
			text := string(tokenizer.Text())
			if inPre() {
				parts := strings.Split(text, "\n")
				for index, part := range parts {
					if index > 0 {
						flush()
					}
					current.WriteString(part)
				}
			} else {
				// The text of a line stays as it is, so that a pull gives back the
				// text of a push. Only the formatting between two tags is ignored.
				text = strings.ReplaceAll(text, " ", " ")
				if strings.TrimSpace(text) == "" {
					if current.Len() == 0 {
						continue
					}
					if strings.Contains(text, "\n") {
						text = " "
					}
				}
				current.WriteString(strings.ReplaceAll(text, "\n", " "))
			}
			if open := top(); open != nil {
				open.endsWithBR = false
			}

		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			tag := string(name)
			if tag == "br" {
				flush()
				if open := top(); open != nil {
					open.endsWithBR = true
				}
				continue
			}
			if !isBlockTag(tag) || tokenType == xhtml.SelfClosingTagToken {
				continue
			}
			if current.Len() > 0 {
				flush()
			}
			parent := top()
			if parent != nil {
				parent.hasChildren = true
			}
			stack = append(stack, &block{tag: tag})
			if tag == "li" && parent != nil {
				if parent.tag == "ol" {
					parent.items++
					current.WriteString(strconv.Itoa(parent.items) + ". ")
				} else {
					current.WriteString("- ")
				}
			}

		case xhtml.EndTagToken:
			name, _ := tokenizer.TagName()
			tag := string(name)
			if !isBlockTag(tag) || len(stack) == 0 {
				continue
			}
			// Close the blocks up to the matching tag, for malformed HTML.
			for len(stack) > 0 {
				open := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				switch {
				case current.Len() > 0:
					flush()
				case !open.hasChildren && !open.endsWithBR && open.tag != "ul" && open.tag != "ol":
					flush() // an empty block is a blank line
				}
				if open.tag == tag {
					break
				}
			}
		}
	}
}

func isBlockTag(tag string) bool {
	switch tag {
	case "p", "div", "li", "ul", "ol", "blockquote", "pre", "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	}
	return false
}

// CanonicalText normalizes the line endings of a text, removes the white space
// at the end of each line, and removes the blank lines at the start and at the end.
func CanonicalText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " \t\r")
	}
	start, end := 0, len(lines)
	for start < end && lines[start] == "" {
		start++
	}
	for end > start && lines[end-1] == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// cardText returns the description of a card as plain text. It reads the rich
// text when the card has it, because the plain text of Fizzy loses the blank lines.
func cardText(card *Card) string {
	if card.DescriptionHTML != "" {
		return DescriptionFromHTML(card.DescriptionHTML)
	}
	return CanonicalText(card.Description)
}
