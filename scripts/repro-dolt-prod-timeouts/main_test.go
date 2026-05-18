package main

import "testing"

func TestIsDriverReadTimeout(t *testing.T) {
	result := opResult{
		StderrTail: "[mysql] 2026/05/13 18:13:48 packets.go:58 read tcp 127.0.0.1:39308->127.0.0.1:21791: i/o timeout",
	}
	if !isDriverReadTimeout(result) {
		t.Fatal("expected MySQL driver read timeout to be classified")
	}
}

func TestIsDriverReadTimeoutIgnoresHarnessTimeout(t *testing.T) {
	result := opResult{
		TimedOut:   true,
		StderrTail: "signal: killed",
	}
	if isDriverReadTimeout(result) {
		t.Fatal("harness timeout should not be classified as driver read timeout")
	}
}

func TestMixedBackgroundJobsIncludesSessionLoadShapes(t *testing.T) {
	jobs := mixedBackgroundJobs(12)
	seen := map[string]bool{}
	for _, job := range jobs {
		seen[job.Kind] = true
	}
	for _, want := range []string{"session-ready", "control-ready", "route-ready", "show", "list", "claim"} {
		if !seen[want] {
			t.Fatalf("mixed background jobs missing %q; seen=%v", want, seen)
		}
	}
}

func TestDepFixtureIssueCountIncludesChainTargets(t *testing.T) {
	if got := depFixtureIssueCount(10, 0); got != 20 {
		t.Fatalf("without chains got %d, want 20", got)
	}
	if got := depFixtureIssueCount(10, 100); got != 1132 {
		t.Fatalf("with chains got %d, want 1132", got)
	}
}
