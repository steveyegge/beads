package main

import (
	"fmt"
	"math"
)

// Birthday paradox: P(collision) ≈ 1 - e^(-n²/2N)
// where n = number of items, N = total possible values
func collisionProbability(numIssues int, idLength int) float64 {
	base := 36.0 // lowercase alphanumeric
	totalPossibilities := math.Pow(base, float64(idLength))
	exponent := -float64(numIssues*numIssues) / (2.0 * totalPossibilities)
	return 1.0 - math.Exp(exponent)
}

// Find the expected number of collisions
func expectedCollisions(numIssues int, idLength int) float64 {
	// Expected number of pairs that collide
	totalPairs := float64(numIssues * (numIssues - 1) / 2)
	return totalPairs * (1.0 / math.Pow(36, float64(idLength)))
}

// Find optimal ID length for a given database size and max collision probability
func optimalIdLength(numIssues int, maxCollisionProb float64) int {
	for length := 3; length <= 12; length++ {
		prob := collisionProbability(numIssues, length)
		if prob <= maxCollisionProb {
			return length
		}
	}
	return 12 // fallback
}

func main() {
	fmt.Println("=== Collision Probability Analysis ===")

	dbSizes := []int{50, 100, 200, 500, 1000, 2000, 5000, 10000}
	idLengths := []int{4, 5, 6, 7, 8}

	// Print table header
	fmt.Printf("%-10s", "DB Size")
	for _, length := range idLengths {
		fmt.Printf("%8d-char", length)
	}
	fmt.Println()
	fmt.Println("----------------------------------------------------------")

	// Print collision probabilities
	for _, size := range dbSizes {
		fmt.Printf("%-10d", size)
		for _, length := range idLengths {
			prob := collisionProbability(size, length)
			fmt.Printf("%11.2f%%", prob*100)
		}
		fmt.Println()
	}

	fmt.Println("\n=== Recommended ID Length by Threshold ===")

	thresholds := []float64{0.10, 0.25, 0.50}
	fmt.Printf("%-10s", "DB Size")
	for _, threshold := range thresholds {
		fmt.Printf("%10.0f%%", threshold*100)
	}
	fmt.Println()
	fmt.Println("----------------------------------")

	for _, size := range dbSizes {
		fmt.Printf("%-10d", size)
		for _, threshold := range thresholds {
			optimal := optimalIdLength(size, threshold)
			fmt.Printf("%10d", optimal)
		}
		fmt.Println()
	}

	fmt.Println("\n=== Expected Number of Collisions ===")
	fmt.Printf("%-10s", "DB Size")
	for _, length := range idLengths {
		fmt.Printf("%10d-char", length)
	}
	fmt.Println()
	fmt.Println("----------------------------------------------------------")

	for _, size := range dbSizes {
		fmt.Printf("%-10d", size)
		for _, length := range idLengths {
			expected := expectedCollisions(size, length)
			fmt.Printf("%14.2f", expected)
		}
		fmt.Println()
	}

	fmt.Println("\n=== Adaptive Scaling Strategy ===")
	fmt.Println("Threshold: 25% collision probability")
	fmt.Printf("%-15s %-12s %-20s\n", "DB Size Range", "ID Length", "Collision Prob")
	fmt.Println("-------------------------------------------------------")

	ranges := []struct {
		min, max int
	}{
		{0, 50},
		{51, 150},
		{151, 500},
		{501, 1500},
		{1501, 5000},
		{5001, 15000},
	}

	threshold := 0.25
	for _, r := range ranges {
		optimal := optimalIdLength(r.max, threshold)
		prob := collisionProbability(r.max, optimal)
		fmt.Printf("%-15s %-12d %18.2f%%\n",
			fmt.Sprintf("%d-%d", r.min, r.max),
			optimal,
			prob*100)
	}
}
