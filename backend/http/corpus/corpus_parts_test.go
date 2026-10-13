//go:build cgo

package corpus_test

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
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

// A part test wired to another part's number would run that part's shapes
// twice and drop its own without a word; testServedCorpus refuses to run
// unless the test's name is its part's (corpusPartNameErr).
func TestCorpusPartNameMustMatchItsPart(t *testing.T) {
	if err := corpusPartNameErr("TestServedCorpusPart3", 3); err != nil {
		t.Errorf("matching name: %v", err)
	}
	for _, c := range []struct {
		name string
		part int
	}{
		{"TestServedCorpusPart3", 2},
		{"TestServedCorpusPart1", 10},
		{"TestServedCorpusPart10", 1},
		{"TestServedCorpusPart3/sub", 3},
		{"TestServedCorpus", 1},
	} {
		if err := corpusPartNameErr(c.name, c.part); err == nil {
			t.Errorf("corpusPartNameErr(%q, %d) = nil, want a mismatch", c.name, c.part)
		}
	}
	// Every wired part test passes the guard: run each with the engine
	// off, so the guard (which comes first) is all that can fail it.
	for k, fn := range servedCorpusParts {
		want := fmt.Sprintf("TestServedCorpusPart%d", k+1)
		if got := testFuncName(fn); got != want {
			t.Errorf("servedCorpusParts[%d] is %s, want %s", k, got, want)
		}
	}
}

// testFuncName is fn's unqualified function name.
func testFuncName(fn func(*testing.T)) string {
	full := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	return full[strings.LastIndex(full, ".")+1:]
}
