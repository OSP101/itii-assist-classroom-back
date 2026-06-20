package utils

import (
	"fmt"
	"math"
)

const scoreDecimalPlaces = 2

func ValidateScoreValue(score float64, maxScore float64) error {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return fmt.Errorf("score must be a valid number")
	}

	rounded := math.Round(score*100) / 100
	if math.Abs(score-rounded) > 1e-9 {
		return fmt.Errorf("score must have at most %d decimal places", scoreDecimalPlaces)
	}

	if score < 0 {
		return fmt.Errorf("score must be at least 0")
	}

	if maxScore >= 0 && score > maxScore {
		return fmt.Errorf("score must be between 0 and %.2f", maxScore)
	}

	return nil
}
