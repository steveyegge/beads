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

// sizedMemories returns n memories whose uncapped full section is about size bytes.
func sizedMemories(n, size int) map[string]string {
	m := make(map[string]string, n)
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("mem-%02d", i)] = strings.Repeat("x", size/n)
	}
	return m
}

func TestRenderPrimeMemoryPlaneWithinRoomIsUnchanged(t *testing.T) {
	stubPrimeMemoryConfig(t, nil, nil)
	memories := testMemories(5)
	for _, compact := range []bool{false, true} {
		want := renderPrimeMemories(memories, compact, 0, 0)
		if got := renderPrimeMemoryPlane(memories, compact, len(want)); got != want {
			t.Fatalf("compact=%v: a section that exactly fits must render as before\ngot:  %q\nwant: %q", compact, got, want)
		}
	}
}

func TestRenderPrimeMemoryPlaneOverRoomShowsBannerOnly(t *testing.T) {
	stubPrimeMemoryConfig(t, nil, nil)
	memories := sizedMemories(4, 4000)
	section := renderPrimeMemories(memories, false, 0, 0)

	out := renderPrimeMemoryPlane(memories, false, len(section)-1)
	if !strings.Contains(out, "## Persistent Memories (4, not shown)\n") {
		t.Fatalf("expected the not-shown header, got %q", out)
	}
	if !strings.Contains(out, fmt.Sprintf("These memories take %d bytes; this output has %d left", len(section), len(section)-1)) {
		t.Fatalf("banner must give the size and the room, got %q", out)
	}
	if !strings.Contains(out, "read one in full with `bd recall <key>`") {
		t.Fatalf("banner must point at bd recall, got %q", out)
	}
	if strings.Contains(out, "### mem-") || strings.Contains(out, "xxxx") {
		t.Fatalf("no entry may be injected past the room, got %q", out)
	}

	compactSection := renderPrimeMemories(memories, true, 0, 0)
	compact := renderPrimeMemoryPlane(memories, true, len(compactSection)-1)
	if !strings.Contains(compact, "## Memories (4, not shown)\n") || strings.Contains(compact, "**mem-") {
		t.Fatalf("compact output must show the banner only, got %q", compact)
	}

	if out := renderPrimeMemoryPlane(memories, false, -50); !strings.Contains(out, "this output has 0 left") {
		t.Fatalf("a negative room must read as none left, got %q", out)
	}
}

// The room depends on what else the output carries: a memories-only hook has
// nearly the whole limit, the full workflow text leaves much less.
func TestPrimeMemoryRoomDependsOnTheRestOfTheOutput(t *testing.T) {
	stubPrimeMemoryConfig(t, nil, nil)
	workflowText := strings.Repeat("w", 6200)

	large := sizedMemories(8, 8000)
	if out := renderPrimeMemoryPlane(large, false, primeMemoryRoom(primeTruncationDirective)); strings.Count(out, "### mem-") != 8 {
		t.Fatalf("an 8 KB store must fit a memories-only output, got %q", out)
	}
	if out := renderPrimeMemoryPlane(large, false, primeMemoryRoom(workflowText)); !strings.Contains(out, "not shown") {
		t.Fatalf("an 8 KB store must not fit next to 6.2 KB of workflow text, got %q", out)
	}

	medium := sizedMemories(5, 3500)
	if out := renderPrimeMemoryPlane(medium, false, primeMemoryRoom(workflowText)); strings.Count(out, "### mem-") != 5 {
		t.Fatalf("a 3.5 KB store must fit next to 6.2 KB of workflow text, got %q", out)
	}
}

func TestRenderPrimeMemoryPlaneConfiguredCapIgnoresRoom(t *testing.T) {
	memories := sizedMemories(4, 4000)

	stubPrimeMemoryConfig(t, map[string]int{"prime.max-memory-chars": 0}, nil)
	if out := renderPrimeMemoryPlane(memories, false, 0); !strings.Contains(out, "## Persistent Memories (4)\n") || strings.Count(out, "### mem-") != 4 {
		t.Fatalf("prime.max-memory-chars: 0 must inject every memory, got %q", out)
	}

	stubPrimeMemoryConfig(t, map[string]int{"prime.max-memories": 2}, nil)
	if out := renderPrimeMemoryPlane(memories, false, 0); !strings.Contains(out, "showing 2 of 4") {
		t.Fatalf("a configured count cap must apply instead of the room, got %q", out)
	}

	stubPrimeMemoryConfig(t, nil, nil)
	primeMaxMemoryChars, primeMaxMemoryCharsSet = 0, true
	if out := renderPrimeMemoryPlane(memories, false, 0); strings.Count(out, "### mem-") != 4 {
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
