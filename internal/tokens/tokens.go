// Package tokens estimates response size in tokens: one quarter token per
// ASCII rune and one token per non-ASCII rune, deliberately conservative
// for Chinese.
package tokens

import (
	"encoding/json"
	"math"
)

// Estimate approximates the token count of v's JSON encoding.
func Estimate(v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return EstimateText(string(data))
}

// EstimateText approximates the token count of text.
func EstimateText(text string) int {
	var tokens float64
	for _, r := range text {
		if r < 0x80 {
			tokens += 0.25
		} else {
			tokens++
		}
	}
	return int(math.Ceil(tokens))
}
