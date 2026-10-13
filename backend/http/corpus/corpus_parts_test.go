//go:build cgo

package corpus_test

import (
	"fmt"
	"testing"
)

// TestCorpusPartsDealEveryShapeOnce runs without the embedded engine. It
// holds the deal TestServedCorpusPart1..N run to what makes splitting the
// corpus safe: every shape lands in exactly one part, each part keeps corpus
// order (so a part's reads still run before its writes, on its own fresh
// seed), no part is empty, and there is one TestServedCorpusPartN per part.
func TestCorpusPartsDealEveryShapeOnce(t *testing.T) {
	if len(servedCorpusParts) != corpusParts {
		t.Fatalf("servedCorpusParts has %d tests, want corpusParts = %d", len(servedCorpusParts), corpusParts)
	}
	c, _ := loadCorpus(t)
	index := map[string]int{}
	for i, s := range c.Shapes {
		index[s.ID] = i
	}
	seen := map[string]int{}
	for part := 1; part <= corpusParts; part++ {
		shapes := corpusPart(c.Shapes, part)
		if len(shapes) == 0 {
			t.Errorf("part %d runs no shapes", part)
		}
		last := -1
		for _, s := range shapes {
			if prev, dup := seen[s.ID]; dup {
				t.Errorf("shape %s is in part %d and part %d", s.ID, prev, part)
			}
			seen[s.ID] = part
			i, ok := index[s.ID]
			if !ok {
				t.Errorf("part %d runs %s, which is not a corpus shape", part, s.ID)
				continue
			}
			if i <= last {
				t.Errorf("part %d runs %s out of corpus order", part, s.ID)
			}
			last = i
		}
	}
	for _, s := range c.Shapes {
		if _, ok := seen[s.ID]; !ok {
			t.Errorf("shape %s is in no part", s.ID)
		}
	}
}

// The deal itself, on a corpus of 10 against 4 parts: round robin in corpus
// order.
func TestCorpusPartIsRoundRobinInOrder(t *testing.T) {
	var shapes []shape
	for i := 0; i < 10; i++ {
		shapes = append(shapes, shape{ID: fmt.Sprint(i)})
	}
	want := map[int]string{1: "[0 4 8]", 2: "[1 5 9]", 3: "[2 6]", 4: "[3 7]"}
	for part, w := range want {
		var ids []string
		for _, s := range corpusPartOf(shapes, part, 4) {
			ids = append(ids, s.ID)
		}
		if got := fmt.Sprint(ids); got != w {
			t.Errorf("part %d of 4 = %s, want %s", part, got, w)
		}
	}
}
