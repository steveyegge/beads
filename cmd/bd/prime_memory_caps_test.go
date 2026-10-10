package main

import (
	"fmt"
	"strings"
	"testing"
)

func testMemories(n int) map[string]string {
	m := make(map[string]string, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("mem-%02d", i)
		m[key] = fmt.Sprintf("insight body for %s", key)
	}
	return m
}

func TestRenderPrimeMemoriesUncappedMatchesLegacyShape(t *testing.T) {
	out := renderPrimeMemories(testMemories(3), false, 0, 0)

	if !strings.Contains(out, "## Persistent Memories (3)\n") {
		t.Fatalf("expected legacy uncapped header, got %q", out)
	}
	if strings.Contains(out, "not shown") || strings.Contains(out, "showing") {
		t.Fatalf("uncapped output must not carry an elision banner, got %q", out)
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(out, fmt.Sprintf("### mem-%02d\n", i)) {
			t.Fatalf("missing memory %d in %q", i, out)
		}
	}
}

func TestRenderPrimeMemoriesCountCap(t *testing.T) {
	out := renderPrimeMemories(testMemories(5), false, 2, 0)

	if !strings.Contains(out, "## Persistent Memories (showing 2 of 5, alphabetical)\n") {
		t.Fatalf("expected capped header, got %q", out)
	}
	if !strings.Contains(out, "> 3 more memories are not shown here (capped by max-memories=2).") {
		t.Fatalf("expected elision banner, got %q", out)
	}
	if !strings.Contains(out, "### mem-00\n") || !strings.Contains(out, "### mem-01\n") {
		t.Fatalf("expected first two alphabetical memories, got %q", out)
	}
	if strings.Contains(out, "### mem-02\n") {
		t.Fatalf("memory beyond the cap leaked into output: %q", out)
	}
	if banner, entries := strings.Index(out, "not shown"), strings.Index(out, "### mem-00"); banner > entries {
		t.Fatalf("elision banner must precede entries so host truncation cannot hide it: %q", out)
	}
}

func TestRenderPrimeMemoriesCharCapStopsAtWholeMemoryBoundary(t *testing.T) {
	memories := testMemories(4)
	oneEntry := len(fmt.Sprintf("### mem-00\n%s\n\n", memories["mem-00"]))
	out := renderPrimeMemories(memories, false, 0, oneEntry+5)

	if !strings.Contains(out, "showing 1 of 4") {
		t.Fatalf("expected exactly one memory within budget, got %q", out)
	}
	if !strings.Contains(out, "capped by max-memory-chars=") {
		t.Fatalf("banner must name the character cap, got %q", out)
	}
	if strings.Contains(out, "### mem-01\n") {
		t.Fatalf("second memory should not fit the budget: %q", out)
	}
}

func TestRenderPrimeMemoriesCharCapAlwaysEmitsFirstMemory(t *testing.T) {
	memories := map[string]string{"huge": strings.Repeat("x", 4096)}
	out := renderPrimeMemories(memories, false, 0, 100)

	if !strings.Contains(out, "### huge\n") {
		t.Fatalf("an oversized first memory must still be emitted, got %q", out)
	}
	if strings.Contains(out, "not shown") {
		t.Fatalf("nothing was elided, banner must be absent: %q", out)
	}
}

func TestRenderPrimeMemoriesCompactCaps(t *testing.T) {
	out := renderPrimeMemories(testMemories(4), true, 3, 0)

	if !strings.Contains(out, "## Memories (showing 3 of 4)\n") {
		t.Fatalf("expected compact capped header, got %q", out)
	}
	if !strings.Contains(out, "- 1 more not shown (capped by max-memories=3); browse with `bd memories <keyword>`\n") {
		t.Fatalf("expected compact elision line, got %q", out)
	}
	if strings.Contains(out, "mem-03") {
		t.Fatalf("memory beyond the cap leaked into compact output: %q", out)
	}
}

func TestRenderPrimeMemoriesCompactUncappedUnchanged(t *testing.T) {
	out := renderPrimeMemories(testMemories(2), true, 0, 0)

	if !strings.Contains(out, "\n## Memories\n") || strings.Contains(out, "showing") {
		t.Fatalf("uncapped compact output changed shape: %q", out)
	}
}

func TestRenderPrimeMemoriesBannerNamesOnlyBindingCap(t *testing.T) {
	// 5 memories, count cap = 50 (won't bind), char cap small enough to stop at ~2 entries.
	// One entry is roughly: "### mem-00\ninsight body for mem-00\n\n" ~ 40+ bytes.
	// Set char cap to allow only 1 entry so char cap fires, not count cap.
	memories := testMemories(5)
	firstEntry := fmt.Sprintf("### mem-00\n%s\n\n", memories["mem-00"])
	smallCharBudget := len(firstEntry) + 5 // fits exactly 1 entry

	out := renderPrimeMemories(memories, false, 50, smallCharBudget)

	if !strings.Contains(out, "capped by max-memory-chars=") {
		t.Fatalf("banner must name the char cap that fired, got %q", out)
	}
	if strings.Contains(out, "max-memories") {
		t.Fatalf("banner must NOT name max-memories when it did not fire, got %q", out)
	}
}

// stubPrimeMemoryConfig clears the memory flags and serves config from the
// given maps for the duration of the test.
func stubPrimeMemoryConfig(t *testing.T, ints map[string]int, bools map[string]bool) {
	t.Helper()
	oldMax, oldChars, oldNo := primeMaxMemories, primeMaxMemoryChars, primeNoMemories
	oldMaxSet, oldCharsSet, oldNoSet := primeMaxMemoriesSet, primeMaxMemoryCharsSet, primeNoMemoriesSet
	oldInt, oldBool, oldIsSet := primeConfigInt, primeConfigBool, primeConfigIsSet
	t.Cleanup(func() {
		primeMaxMemories, primeMaxMemoryChars, primeNoMemories = oldMax, oldChars, oldNo
		primeMaxMemoriesSet, primeMaxMemoryCharsSet, primeNoMemoriesSet = oldMaxSet, oldCharsSet, oldNoSet
		primeConfigInt, primeConfigBool, primeConfigIsSet = oldInt, oldBool, oldIsSet
	})
	primeMaxMemories, primeMaxMemoryChars, primeNoMemories = 0, 0, false
	primeMaxMemoriesSet, primeMaxMemoryCharsSet, primeNoMemoriesSet = false, false, false
	primeConfigInt = func(key string) int { return ints[key] }
	primeConfigBool = func(key string) bool { return bools[key] }
	primeConfigIsSet = func(key string) bool {
		_, isInt := ints[key]
		_, isBool := bools[key]
		return isInt || isBool
	}
}

func TestPrimeMemoryCapsResolution(t *testing.T) {
	configValues := map[string]int{}
	stubPrimeMemoryConfig(t, configValues, nil)

	// No flags, no config: uncapped and not configured.
	if c, ch, configured := primeMemoryCaps(); c != 0 || ch != 0 || configured {
		t.Fatalf("expected uncapped, unconfigured defaults, got %d/%d/%v", c, ch, configured)
	}

	// Config keys apply when flags are absent.
	configValues["prime.max-memories"] = 20
	configValues["prime.max-memory-chars"] = 25000
	if c, ch, configured := primeMemoryCaps(); c != 20 || ch != 25000 || !configured {
		t.Fatalf("expected configured caps 20/25000, got %d/%d/%v", c, ch, configured)
	}

	// An explicit flag wins over config.
	primeMaxMemories, primeMaxMemoriesSet = 5, true
	if c, _, _ := primeMemoryCaps(); c != 5 {
		t.Fatalf("flag must beat config, got %d", c)
	}

	// An explicit 0 flag forces unlimited despite config.
	primeMaxMemories = 0
	if c, _, _ := primeMemoryCaps(); c != 0 {
		t.Fatalf("explicit --max-memories 0 must force unlimited, got %d", c)
	}

	// Negative values clamp to uncapped.
	primeMaxMemories, primeMaxMemoriesSet = -3, true
	primeMaxMemoryChars, primeMaxMemoryCharsSet = -1, true
	if c, ch, _ := primeMemoryCaps(); c != 0 || ch != 0 {
		t.Fatalf("negative caps must clamp to 0, got %d/%d", c, ch)
	}
}

func TestPrimeMemoryCapsConfiguredByExplicitZero(t *testing.T) {
	stubPrimeMemoryConfig(t, map[string]int{"prime.max-memory-chars": 0}, nil)
	if c, ch, configured := primeMemoryCaps(); c != 0 || ch != 0 || !configured {
		t.Fatalf("a config key set to 0 must count as configured and uncapped, got %d/%d/%v", c, ch, configured)
	}
}

// bigMemories returns n memories whose entries together exceed the default budget.
func bigMemories(n int) map[string]string {
	m := make(map[string]string, n)
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("mem-%02d", i)] = strings.Repeat("x", primeDefaultMemoryBudget/n+1)
	}
	return m
}

func TestRenderPrimeMemoryPlaneUnderDefaultBudgetIsUnchanged(t *testing.T) {
	stubPrimeMemoryConfig(t, nil, nil)
	memories := testMemories(5)
	if primeMemoryEntriesSize(memories, false) > primeDefaultMemoryBudget {
		t.Fatal("fixture must fit the default budget")
	}
	for _, compact := range []bool{false, true} {
		if got, want := renderPrimeMemoryPlane(memories, compact), renderPrimeMemories(memories, compact, 0, 0); got != want {
			t.Fatalf("compact=%v: a store within the default budget must render as before\ngot:  %q\nwant: %q", compact, got, want)
		}
	}
}

func TestRenderPrimeMemoryPlaneOverDefaultBudgetShowsBannerOnly(t *testing.T) {
	stubPrimeMemoryConfig(t, nil, nil)
	memories := bigMemories(4)
	total := primeMemoryEntriesSize(memories, false)
	if total <= primeDefaultMemoryBudget {
		t.Fatalf("fixture must exceed the default budget, got %d bytes", total)
	}

	out := renderPrimeMemoryPlane(memories, false)
	if !strings.Contains(out, "## Persistent Memories (4, not shown)\n") {
		t.Fatalf("expected the not-shown header, got %q", out)
	}
	if !strings.Contains(out, fmt.Sprintf("These memories total %d bytes, over the %d-byte default budget", total, primeDefaultMemoryBudget)) {
		t.Fatalf("banner must give the total and the budget, got %q", out)
	}
	if !strings.Contains(out, "read one in full with `bd recall <key>`") {
		t.Fatalf("banner must point at bd recall, got %q", out)
	}
	if strings.Contains(out, "### mem-") || strings.Contains(out, "xxxx") {
		t.Fatalf("no entry may be injected over the default budget, got %q", out)
	}

	// Compact entries are cut to 150 characters, so it takes more of them.
	many := bigMemories(30)
	if primeMemoryEntriesSize(many, true) <= primeDefaultMemoryBudget {
		t.Fatal("compact fixture must exceed the default budget")
	}
	compact := renderPrimeMemoryPlane(many, true)
	if !strings.Contains(compact, "## Memories (30, not shown)\n") || strings.Contains(compact, "**mem-") {
		t.Fatalf("compact output must show the banner only, got %q", compact)
	}
}

func TestRenderPrimeMemoryPlaneConfiguredCapReplacesDefaultBudget(t *testing.T) {
	memories := bigMemories(4)

	stubPrimeMemoryConfig(t, map[string]int{"prime.max-memory-chars": 0}, nil)
	if out := renderPrimeMemoryPlane(memories, false); !strings.Contains(out, "## Persistent Memories (4)\n") || strings.Count(out, "### mem-") != 4 {
		t.Fatalf("prime.max-memory-chars: 0 must inject every memory, got %q", out)
	}

	stubPrimeMemoryConfig(t, map[string]int{"prime.max-memories": 2}, nil)
	if out := renderPrimeMemoryPlane(memories, false); !strings.Contains(out, "showing 2 of 4") {
		t.Fatalf("a configured count cap must apply instead of the default budget, got %q", out)
	}

	stubPrimeMemoryConfig(t, nil, nil)
	primeMaxMemoryChars, primeMaxMemoryCharsSet = 0, true
	if out := renderPrimeMemoryPlane(memories, false); strings.Count(out, "### mem-") != 4 {
		t.Fatalf("--max-memory-chars 0 must inject every memory, got %q", out)
	}
}

func TestPrimeMemoriesOff(t *testing.T) {
	stubPrimeMemoryConfig(t, nil, nil)
	if primeMemoriesOff() {
		t.Fatal("memories must be on with no flag and no config")
	}

	primeNoMemories, primeNoMemoriesSet = true, true
	if !primeMemoriesOff() {
		t.Fatal("--no-memories must turn memories off")
	}

	stubPrimeMemoryConfig(t, nil, map[string]bool{"prime.no-memories": true})
	if !primeMemoriesOff() {
		t.Fatal("prime.no-memories: true must turn memories off without a flag")
	}

	primeMaxMemories, primeMaxMemoriesSet = 3, true
	if primeMemoriesOff() {
		t.Fatal("--max-memories must override prime.no-memories")
	}

	primeMaxMemoriesSet = false
	primeNoMemories, primeNoMemoriesSet = false, true
	if primeMemoriesOff() {
		t.Fatal("--no-memories=false must override prime.no-memories")
	}
}
