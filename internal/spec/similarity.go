package spec

import (
	"sort"
	"strings"
	"unicode"
)

type DuplicatePair struct {
	SpecA      string  `json:"spec_a"`
	SpecB      string  `json:"spec_b"`
	Similarity float64 `json:"similarity"`
	Key        string  `json:"key,omitempty"`
}

// FindDuplicates returns pairs with similarity >= threshold.
func FindDuplicates(specs []SpecRegistryEntry, threshold float64) []DuplicatePair {
	if threshold <= 0 {
		threshold = 0.85
	}
	results := make([]DuplicatePair, 0)

	for i := 0; i < len(specs); i++ {
		tokensA := tokenizeSpec(specs[i])
		if len(tokensA) == 0 {
			continue
		}
		for j := i + 1; j < len(specs); j++ {
			tokensB := tokenizeSpec(specs[j])
			if len(tokensB) == 0 {
				continue
			}
			sim := jaccard(tokensA, tokensB)
			if sim < threshold {
				continue
			}
			results = append(results, DuplicatePair{
				SpecA:      specs[i].SpecID,
				SpecB:      specs[j].SpecID,
				Similarity: sim,
				Key:        commonKey(tokensA, tokensB),
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Similarity != results[j].Similarity {
			return results[i].Similarity > results[j].Similarity
		}
		if results[i].SpecA != results[j].SpecA {
			return results[i].SpecA < results[j].SpecA
		}
		return results[i].SpecB < results[j].SpecB
	})
	return results
}

func tokenizeSpec(entry SpecRegistryEntry) map[string]struct{} {
	text := strings.TrimSpace(entry.Title + " " + entry.Summary)
	if text == "" {
		return nil
	}
	normalized := normalizeText(text)
	parts := strings.Fields(normalized)
	if len(parts) == 0 {
		return nil
	}
	tokens := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		if len(p) < 3 {
			continue
		}
		tokens[p] = struct{}{}
	}
	return tokens
}

func normalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// ResolveCanonical determines which spec to keep and which to delete for a duplicate pair.
// Returns (keep, delete, skip). If skip is true, keep and delete are empty.
// Priority: archive(4) > active(3) > reference(2) > root/ideas(1).
// Same priority or unknown directories result in skip.
func ResolveCanonical(pair DuplicatePair) (keep, del string, skip bool) {
	dirA := canonicalDir(pair.SpecA)
	dirB := canonicalDir(pair.SpecB)

	// Unknown = ambiguous, skip
	if dirA == "unknown" || dirB == "unknown" {
		return "", "", true
	}

	// Same priority = ambiguous, skip
	priority := map[string]int{"archive": 4, "active": 3, "reference": 2, "root": 1, "ideas": 1}
	if priority[dirA] == priority[dirB] {
		return "", "", true
	}

	if priority[dirA] > priority[dirB] {
		return pair.SpecA, pair.SpecB, false
	}
	return pair.SpecB, pair.SpecA, false
}

// canonicalDir extracts the canonical directory from a spec path.
// Returns "root" for specs directly in specs/ with no subdirectory.
func canonicalDir(specID string) string {
	parts := strings.Split(specID, "/")
	for _, p := range parts {
		switch p {
		case "active", "archive", "reference", "ideas":
			return p
		}
	}
	// If path starts with "specs/" and has exactly 2 parts (specs/FILE.md), it's root
	if len(parts) == 2 && parts[0] == "specs" {
		return "root"
	}
	return "unknown"
}

func commonKey(a, b map[string]struct{}) string {
	common := make([]string, 0)
	for k := range a {
		if _, ok := b[k]; ok {
			common = append(common, k)
		}
	}
	sort.Strings(common)
	if len(common) == 0 {
		return ""
	}
	if len(common) > 6 {
		common = common[:6]
	}
	return strings.Join(common, " ")
}
