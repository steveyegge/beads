package spec

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ScoredMatch is a suggested spec match with a similarity score.
type ScoredMatch struct {
	SpecID string  `json:"spec_id"`
	Title  string  `json:"title"`
	Score  float64 `json:"score"`
}

var tokenRegex = regexp.MustCompile(`\w+`)

var stopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "for": {}, "to": {},
	"in": {}, "on": {}, "with": {}, "spec": {}, "feature": {}, "task": {},
	"bug": {}, "implement": {}, "add": {}, "fix": {}, "update": {}, "full": {},
	"complete": {}, "new": {},
}

// Tokenize extracts normalized tokens from a string, removing stopwords.
func Tokenize(s string) []string {
	words := tokenRegex.FindAllString(strings.ToLower(s), -1)
	if len(words) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) <= 2 {
			continue
		}
		if _, ok := stopwords[w]; ok {
			continue
		}
		tokens = append(tokens, w)
	}
	return tokens
}

// JaccardSimilarity returns the Jaccard similarity of two token sets.
func JaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]struct{}, len(a))
	for _, t := range a {
		setA[t] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, t := range b {
		setB[t] = struct{}{}
	}
	intersection := 0
	for t := range setA {
		if _, ok := setB[t]; ok {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// SuggestSpecs returns scored spec suggestions based on an issue title.
func SuggestSpecs(issueTitle string, specs []SpecRegistryEntry, limit int, minScore float64) []ScoredMatch {
	if limit <= 0 {
		limit = 3
	}
	issueTokens := Tokenize(issueTitle)
	if len(issueTokens) == 0 {
		return nil
	}

	matches := make([]ScoredMatch, 0, len(specs))
	for _, spec := range specs {
		if spec.SpecID == "" {
			continue
		}
		titleScore := JaccardSimilarity(issueTokens, Tokenize(spec.Title))
		filename := filepath.Base(spec.SpecID)
		filename = strings.TrimSuffix(filename, filepath.Ext(filename))
		filenameScore := JaccardSimilarity(issueTokens, Tokenize(filename))
		score := 0.6*titleScore + 0.4*filenameScore
		if score < minScore {
			continue
		}
		matches = append(matches, ScoredMatch{
			SpecID: spec.SpecID,
			Title:  spec.Title,
			Score:  score,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].SpecID < matches[j].SpecID
		}
		return matches[i].Score > matches[j].Score
	})

	if len(matches) > limit {
		return matches[:limit]
	}
	return matches
}

// BestSpecMatch returns the best matching spec above the minScore threshold.
func BestSpecMatch(issueTitle string, specs []SpecRegistryEntry, minScore float64) (ScoredMatch, bool) {
	matches := SuggestSpecs(issueTitle, specs, 1, minScore)
	if len(matches) == 0 {
		return ScoredMatch{}, false
	}
	return matches[0], true
}
