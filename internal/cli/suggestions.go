package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

func unknownValueError(kind, value string, candidates []string, fallback string) error {
	message := fmt.Sprintf("unknown %s %q", kind, value)
	if candidate := closestMatch(value, candidates); candidate != "" {
		message += fmt.Sprintf("; did you mean %q?", candidate)
	} else if fallback != "" {
		message += "; " + fallback
	}
	return errors.New(message)
}

func closestMatch(value string, candidates []string) string {
	value = strings.ToLower(value)
	maximum := 2
	if len([]rune(value)) >= 8 {
		maximum = 3
	}
	best, distance := "", maximum+1
	sorted := slices.Clone(candidates)
	slices.Sort(sorted)
	for _, candidate := range sorted {
		candidateDistance := editDistance(value, strings.ToLower(candidate))
		if candidateDistance < distance {
			best, distance = candidate, candidateDistance
		}
	}
	return best
}

func editDistance(left, right string) int {
	a, b := []rune(left), []rune(right)
	previous := make([]int, len(b)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range a {
		current := make([]int, len(b)+1)
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range b {
			cost := 1
			if leftRune == rightRune {
				cost = 0
			}
			current[rightIndex+1] = min(
				current[rightIndex]+1,
				previous[rightIndex+1]+1,
				previous[rightIndex]+cost,
			)
		}
		previous = current
	}
	return previous[len(b)]
}
