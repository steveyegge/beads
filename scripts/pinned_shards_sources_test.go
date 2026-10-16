package scripts_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

var goTopLevelTestRe = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(t \*testing\.T\) \{`)

// Every name a pinned-shard manifest pins is a top-level test of the
// target's package: a stale name runs nowhere, and a renamed test silently
// falls back to the round-robin shards (regenerate with
// tools/bazel/pin_shards.py).
func TestPinnedShardManifestNamesAreTests(t *testing.T) {
	root := sourceRepoRoot(t)
	for _, tgt := range pinnedShardTargets {
		t.Run(tgt.rule, func(t *testing.T) {
			tests := map[string]bool{}
			srcs, err := filepath.Glob(filepath.Join(root, tgt.pkg, "*_test.go"))
			if err != nil || len(srcs) == 0 {
				t.Fatalf("no *_test.go in %s: %v", tgt.pkg, err)
			}
			for _, src := range srcs {
				b, err := os.ReadFile(src)
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range goTopLevelTestRe.FindAllStringSubmatch(string(b), -1) {
					tests[m[1]] = true
				}
			}
			pinned, _ := readPinnedShardManifest(t, root, tgt.manifest)
			var stale []string
			for name := range pinned {
				if !tests[name] {
					stale = append(stale, name)
				}
			}
			sort.Strings(stale)
			for _, name := range stale {
				t.Errorf("%s pins %s, which is not a top-level test in %s/*_test.go; regenerate it (tools/bazel/pin_shards.py)", tgt.manifest, name, tgt.pkg)
			}
		})
	}
}

var corpusPartsConst = regexp.MustCompile(`(?m)^const corpusParts = (\d+)$`)

// corpus_served_test's manifest is not pin_shards.py's: it pins
// TestServedCorpusPart1..N, the corpus parts (backend/http/corpus
// corpusParts), to shards 1..N, one each, and pins nothing else, so every
// part runs alone in its own Bazel shard on its own seed. One shard is left
// for the binary's round-robin over the package's other (engine-free) tests.
func TestCorpusPartsArePinnedOnePerShard(t *testing.T) {
	root := sourceRepoRoot(t)
	const (
		build    = "backend/http/corpus/BUILD.bazel"
		manifest = "backend/http/corpus/served_pinned_shards.txt"
		src      = "backend/http/corpus/corpus_test.go"
	)
	m := corpusPartsConst.FindStringSubmatch(readPolicyFile(t, root, src))
	if m == nil {
		t.Fatalf("%s has no `const corpusParts = N`", src)
	}
	parts, _ := strconv.Atoi(m[1])
	if parts < 2 {
		t.Fatalf("corpusParts = %d; a single part needs no pinned shards", parts)
	}
	pinned, maxShard := readPinnedShardManifest(t, root, manifest)
	want := map[string]int{}
	for k := 1; k <= parts; k++ {
		want[fmt.Sprintf("TestServedCorpusPart%d", k)] = k
	}
	if !reflect.DeepEqual(pinned, want) {
		t.Errorf("%s pins %v, want each of the %d corpus parts on its own shard: %v", manifest, pinned, parts, want)
	}
	rule := bazelRuleBlock(readPolicyFile(t, root, build), "corpus_served_test")
	sm := shardCountPattern.FindStringSubmatch(rule)
	if sm == nil {
		t.Fatalf("%s:corpus_served_test has no shard_count", build)
	}
	if n, _ := strconv.Atoi(sm[1]); n != maxShard+1 {
		t.Errorf("%s:corpus_served_test shard_count = %d, want %d (%d parts and one round-robin shard)", build, n, maxShard+1, maxShard)
	}
}
