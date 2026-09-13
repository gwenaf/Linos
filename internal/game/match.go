package game

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/gwenaf/linos/internal/pack"
)

// shortAnswer is the length under which no typo is tolerated: "dido" must not match "daho".
const shortAnswer = 4

// matches tells whether a typed or picked answer is right for a guess.
func matches(g *pack.Guess, value string, fuzziness float64) bool {
	switch g.Type {
	case "number":
		v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(value), ",", "."), 64)
		return err == nil && math.Abs(v-g.Answer.(float64)) <= g.Tolerance
	case "choice":
		return normalize(value) == normalize(g.Answer.(string))
	}
	typed := normalize(value)
	for _, answer := range g.Answers {
		if nearlyEqual(typed, normalize(answer), fuzziness) {
			return true
		}
	}
	return false
}

// nearlyEqual accepts up to fuzziness × length typos, none for short answers.
func nearlyEqual(typed, answer string, fuzziness float64) bool {
	n := len([]rune(answer))
	if n < shortAnswer {
		return typed == answer
	}
	return float64(levenshtein([]rune(typed), []rune(answer))) <= fuzziness*float64(n)
}

var stripAccents = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// normalize lowercases, strips accents, drops punctuation ("AC/DC" is "acdc") and collapses spaces.
func normalize(s string) string {
	s, _, _ = transform.String(stripAccents, strings.ToLower(s)) // these transformers cannot fail
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		case unicode.IsSpace(r):
			space = true
		}
	}
	return b.String()
}

// levenshtein counts the single-character insertions, deletions or substitutions between a and b.
func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
