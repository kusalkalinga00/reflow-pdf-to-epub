package corpus

import (
	"strings"
	"unicode"
)

// CharRecall is the fraction of the expected characters that appear in the
// result, ignoring order and whitespace.
//
// It is a bag-of-characters measure on purpose: it answers "did every word
// survive", which is the first thing that goes wrong in PDF extraction, and it
// does not punish a paragraph break landing in a different place.
func CharRecall(want, got string) float64 {
	expected := runeCounts(want)
	if len(expected) == 0 {
		return 1
	}

	total := 0
	for _, n := range expected {
		total += n
	}

	matched := 0
	for r, n := range runeCounts(got) {
		if have, ok := expected[r]; ok {
			matched += min(n, have)
		}
	}

	return float64(matched) / float64(total)
}

func runeCounts(s string) map[rune]int {
	counts := make(map[rune]int, len(s))
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		counts[r]++
	}
	return counts
}

// WordErrorRate is the word-level edit distance divided by the number of
// expected words. It measures substitutions and deletions, which is what a
// reader experiences as wrong words rather than missing ones.
func WordErrorRate(want, got string) float64 {
	expected := words(want)
	if len(expected) == 0 {
		return 0
	}
	return float64(editDistance(expected, words(got))) / float64(len(expected))
}

// dashVariants maps the dash glyphs PDF producers mix through to the plain
// hyphen. A dash's glyph choice is typography, not a different word: "Jun
// 2026 – Present" and "Jun 2026 - Present" are the same words.
var dashVariants = strings.NewReplacer(
	"\u2010", "-",
	"\u2011", "-",
	"\u2012", "-",
	"\u2013", "-",
	"\u2014", "-",
	"\u2015", "-",
)

// words tokenises for comparison, not for display.
//
// Spacing around punctuation is typography, not a different word: "Next.js ,"
// and "Next.js," must not count as an error. Punctuation-only tokens join the
// word before them, and an apostrophe split from its word ("I' m") joins it
// too. Word order still counts, which is the point of the metric.
func words(s string) []string {
	var out []string
	for _, token := range strings.Fields(dashVariants.Replace(strings.ToLower(s))) {
		if n := len(out); n > 0 && (punctOnly(token) || endsWithApostrophe(out[n-1])) {
			out[n-1] += token
			continue
		}
		out = append(out, token)
	}
	return out
}

func endsWithApostrophe(s string) bool {
	return strings.HasSuffix(s, "'") || strings.HasSuffix(s, "\u2019")
}

func punctOnly(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if !strings.ContainsRune(`,.;:!?)]}'"`, r) {
			return false
		}
	}
	return true
}

// editDistance is the Levenshtein distance over tokens, computed with two rows
// so memory stays proportional to the shorter side.
func editDistance(a, b []string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}

	return prev[len(b)]
}

// InOrder reports whether every marker appears in text, in the given order.
func InOrder(text string, markers []string) bool {
	cursor := 0
	for _, m := range markers {
		if m == "" {
			continue
		}
		i := strings.Index(text[cursor:], m)
		if i < 0 {
			return false
		}
		cursor += i + len(m)
	}
	return true
}

func min3(a, b, c int) int {
	return min(a, min(b, c))
}
