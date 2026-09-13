package game

import (
	"testing"

	"github.com/gwenaf/linos/internal/pack"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"  Étienne   DAHO ": "etienne daho",
		"AC/DC":             "acdc",
		"L'Aventurier !":    "laventurier",
		"Take\ton\nMe":      "take on me",
		"":                  "",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"daho", "dido", 2},
		{"the les i know the beter", "the less i know the better", 2},
	}
	for _, tc := range cases {
		if got := levenshtein([]rune(tc.a), []rune(tc.b)); got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestMatches(t *testing.T) {
	text := &pack.Guess{Type: "text", Answers: []string{"The Less I Know The Better", "Tame Impala"}}
	short := &pack.Guess{Type: "text", Answers: []string{"Daho"}}
	choice := &pack.Guess{Type: "choice", Choices: []string{"Take On Me", "Europe"}, Answer: "Take On Me"}
	number := &pack.Guess{Type: "number", Answer: 1984.0, Tolerance: 1}

	cases := []struct {
		name  string
		g     *pack.Guess
		value string
		want  bool
	}{
		{"typos within 20%", text, "the les i know the beter", true},
		{"second answer", text, "tame impala", true},
		{"too many typos", text, "the last one", false},
		{"empty", text, "", false},
		{"short exact with accents", short, "DAHÔ", true},
		{"short with a typo", short, "Dido", false},
		{"choice", choice, "take on me", true},
		{"wrong choice", choice, "Europe", false},
		{"number within tolerance", number, " 1985 ", true},
		{"number with a comma", number, "1983,5", true},
		{"number too far", number, "1990", false},
		{"not a number", number, "mille", false},
	}
	for _, tc := range cases {
		if got := matches(tc.g, tc.value, 0.2); got != tc.want {
			t.Errorf("%s: matches(%q) = %v, want %v", tc.name, tc.value, got, tc.want)
		}
	}
}
