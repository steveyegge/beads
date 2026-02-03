package wobble

import (
	"hash/fnv"
	"math/rand"
	"regexp"
	"strings"
)

// WobbleMetrics contains the behavioral analysis results.
type WobbleMetrics struct {
	Runs           int                `json:"runs"`
	ExactMatchRate float64            `json:"exact_match_rate"`
	VariantCount   int                `json:"variant_count"`
	AvgSimilarity  float64            `json:"avg_similarity"`
	Bias           float64            `json:"bias"`
	Variance       float64            `json:"variance"`
	WobbleScore    float64            `json:"wobble_score"`
	DriftTypes     map[string]int     `json:"drift_types"`
	Variants       []string           `json:"variants"`
}

// DriftType categorizes the type of command drift.
type DriftType string

const (
	DriftExact DriftType = "exact"      // Command matches expected exactly
	DriftFlag  DriftType = "flag_drift" // Same base command, different flags
	DriftFull  DriftType = "full_drift" // Completely different command
)

// SimulateExecutions generates N simulated command variations.
// In production, this would be replaced with actual Claude API calls or log replay.
func SimulateExecutions(skillName, expected string, runs int) []string {
	if expected == "" {
		result := make([]string, runs)
		for i := 0; i < runs; i++ {
			result[i] = "# No default command for " + skillName
		}
		return result
	}

	// Generate realistic drift variations
	variations := generateVariations(expected)

	// Use deterministic seed based on skill name for reproducibility
	h := fnv.New32a()
	h.Write([]byte(skillName))
	seed := int64(h.Sum32())
	rng := rand.New(rand.NewSource(seed))

	result := make([]string, runs)
	for i := 0; i < runs; i++ {
		result[i] = variations[rng.Intn(len(variations))]
	}
	return result
}

// generateVariations creates realistic command drift patterns.
func generateVariations(expected string) []string {
	variations := []string{
		expected, // Correct
		expected, // Correct (weighted)
	}

	// Missing flag variation
	if strings.Contains(expected, "--reverse") {
		variations = append(variations, strings.Replace(expected, "--reverse", "", 1))
	}
	if strings.Contains(expected, "-r") {
		variations = append(variations, strings.Replace(expected, "-r", "", 1))
	}

	// Added flag variation
	variations = append(variations, expected+" --verbose")

	// Changed limit variation
	headPattern := regexp.MustCompile(`head -\d+`)
	if headPattern.MatchString(expected) {
		variations = append(variations, headPattern.ReplaceAllString(expected, "head -20"))
	}

	// Wrong filter variation (common Claude drift)
	filterPattern := regexp.MustCompile(`--\w+-after=\S+`)
	if filterPattern.MatchString(expected) {
		variations = append(variations, filterPattern.ReplaceAllString(expected, "--status=in_progress"))
	}

	return variations
}

// CalculateWobble computes wobble metrics from a list of commands and the expected command.
func CalculateWobble(commands []string, expected string) *WobbleMetrics {
	n := len(commands)
	if n == 0 {
		return &WobbleMetrics{
			DriftTypes: make(map[string]int),
		}
	}

	// Exact match rate
	exactMatches := 0
	for _, cmd := range commands {
		if cmd == expected {
			exactMatches++
		}
	}
	exactMatchRate := float64(exactMatches) / float64(n)

	// Unique variants
	uniqueSet := make(map[string]struct{})
	for _, cmd := range commands {
		uniqueSet[cmd] = struct{}{}
	}
	variants := make([]string, 0, len(uniqueSet))
	for v := range uniqueSet {
		variants = append(variants, v)
	}
	variantCount := len(variants)

	// Calculate bias: systematic deviation from expected
	avgSimilarity := AverageSimilarityTo(commands, expected)
	bias := 1 - avgSimilarity

	// Calculate variance: inconsistency across runs
	variance := 1 - AveragePairwiseSimilarity(commands)

	// Wobble score (incoherence formula from the paper)
	// Wobble = Variance / (Bias² + Variance)
	var wobbleScore float64
	denominator := bias*bias + variance
	if denominator > 0 {
		wobbleScore = variance / denominator
	}

	// Categorize drift types
	driftTypes := categorizeDrift(commands, expected)

	return &WobbleMetrics{
		Runs:           n,
		ExactMatchRate: exactMatchRate,
		VariantCount:   variantCount,
		AvgSimilarity:  avgSimilarity,
		Bias:           bias,
		Variance:       variance,
		WobbleScore:    wobbleScore,
		DriftTypes:     driftTypes,
		Variants:       variants,
	}
}

// categorizeDrift classifies each command into drift categories.
func categorizeDrift(commands []string, expected string) map[string]int {
	driftTypes := make(map[string]int)

	expectedParts := strings.Fields(expected)
	var expectedPrefix []string
	if len(expectedParts) >= 2 {
		expectedPrefix = expectedParts[:2]
	} else {
		expectedPrefix = expectedParts
	}

	for _, cmd := range commands {
		if cmd == expected {
			driftTypes[string(DriftExact)]++
			continue
		}

		cmdParts := strings.Fields(cmd)
		var cmdPrefix []string
		if len(cmdParts) >= 2 {
			cmdPrefix = cmdParts[:2]
		} else {
			cmdPrefix = cmdParts
		}

		// Check if first two parts match (base command same, flags different)
		if len(expectedPrefix) > 0 && len(cmdPrefix) > 0 &&
			strings.Join(expectedPrefix, " ") == strings.Join(cmdPrefix, " ") {
			driftTypes[string(DriftFlag)]++
		} else {
			driftTypes[string(DriftFull)]++
		}
	}

	return driftTypes
}

// GetVerdict determines the stability verdict based on wobble and structural risk scores.
func GetVerdict(wobbleScore, structuralRisk float64) (verdict, recommendation string) {
	combined := (wobbleScore * 0.7) + (structuralRisk * 0.3)

	if combined < 0.2 {
		return "STABLE", "Skill executes consistently. No changes needed."
	} else if combined < 0.5 {
		return "WOBBLY", "Add 'DO NOT IMPROVISE' section. Clarify default."
	}
	return "UNSTABLE", "Rewrite with explicit EXECUTE NOW block."
}

// GetCombinedRisk calculates the combined risk score.
func GetCombinedRisk(wobbleScore, structuralRisk float64) float64 {
	return (wobbleScore * 0.7) + (structuralRisk * 0.3)
}
