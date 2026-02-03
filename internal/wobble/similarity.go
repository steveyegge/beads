// Package wobble detects skill drift before it breaks workflows.
// Based on Anthropic's "Hot Mess of AI" paper on AI incoherence.
package wobble

// SequenceSimilarity calculates the similarity ratio between two strings.
// This is a Go equivalent of Python's difflib.SequenceMatcher.ratio().
// Returns a value between 0.0 (completely different) and 1.0 (identical).
func SequenceSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	// Use longest common subsequence ratio
	lcs := longestCommonSubsequence(a, b)
	return 2.0 * float64(lcs) / float64(len(a)+len(b))
}

// longestCommonSubsequence returns the length of the LCS of two strings.
func longestCommonSubsequence(a, b string) int {
	m, n := len(a), len(b)
	if m == 0 || n == 0 {
		return 0
	}

	// Use two rows instead of full matrix for memory efficiency
	prev := make([]int, n+1)
	curr := make([]int, n+1)

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else {
				curr[j] = max(prev[j], curr[j-1])
			}
		}
		prev, curr = curr, prev
	}

	return prev[n]
}

// AveragePairwiseSimilarity calculates the average similarity between all pairs
// of strings in the given slice. Returns 1.0 if there are fewer than 2 items.
func AveragePairwiseSimilarity(items []string) float64 {
	n := len(items)
	if n < 2 {
		return 1.0
	}

	var total float64
	count := 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			total += SequenceSimilarity(items[i], items[j])
			count++
		}
	}

	if count == 0 {
		return 1.0
	}
	return total / float64(count)
}

// AverageSimilarityTo calculates the average similarity of all items to a reference string.
func AverageSimilarityTo(items []string, reference string) float64 {
	if len(items) == 0 {
		return 0.0
	}

	var total float64
	for _, item := range items {
		total += SequenceSimilarity(reference, item)
	}

	return total / float64(len(items))
}
