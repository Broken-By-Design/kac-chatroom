package main

import (
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Port of utils/censor.py.

var CENSOR_WORDS []string

const CENSOR_CHAR = '#'

func init() {
	b, err := os.ReadFile("badwords.txt")
	if err != nil {
		b = []byte{}
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			CENSOR_WORDS = append(CENSOR_WORDS, strings.ToLower(line))
		}
	}
}

var leetSubstitutions = map[string]string{
	"0": "o", "o": "o",
	"1": "i", "!": "i", "|": "i",
	"3": "e", "ε": "e",
	"4": "a", "@": "a",
	"5": "s", "$": "s",
	"7": "t",
	"8": "b",
	"9": "g", "q": "g",
	"ß": "ss",
}

func normalizeChar(r rune) string {
	s := norm.NFD.String(string(r))
	var sb strings.Builder
	for _, c := range s {
		if unicode.Is(unicode.Mn, c) {
			continue
		}
		sb.WriteRune(c)
	}
	lower := strings.ToLower(sb.String())
	if v, ok := leetSubstitutions[lower]; ok {
		return v
	}
	return lower
}

func normalizeText(text string) string {
	var sb strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteString(normalizeChar(r))
		} else {
			sb.WriteRune(r)
		}
	}
	var sb2 strings.Builder
	for _, r := range sb.String() {
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		sb2.WriteRune(r)
	}
	return strings.ToLower(sb2.String())
}

// findWords splits a message into unicode word tokens (\w+ equivalent).
func findWords(s string) []string {
	var words []string
	var cur strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}

func absInt(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// longestCommonSubstring returns (start in a, start in b, length).
func longestCommonSubstring(a, b string) (int, int, int) {
	ra := []rune(a)
	rb := []rune(b)
	bestLen, bestI, bestJ := 0, 0, 0
	for i := 0; i < len(ra); i++ {
		for j := 0; j < len(rb); j++ {
			k := 0
			for i+k < len(ra) && j+k < len(rb) && ra[i+k] == rb[j+k] {
				k++
			}
			if k > bestLen {
				bestLen, bestI, bestJ = k, i, j
			}
		}
	}
	return bestI, bestJ, bestLen
}

func matchCount(a, b string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	i, j, n := longestCommonSubstring(a, b)
	if n == 0 {
		return 0
	}
	return n + matchCount(a[:i], b[:j]) + matchCount(a[i+n:], b[j+n:])
}

// calculateSimilarity approximates difflib.SequenceMatcher.ratio() using
// Ratcliff/Obershelp matching.
func calculateSimilarity(a, b string) float64 {
	total := len([]rune(a)) + len([]rune(b))
	if total == 0 {
		return 1.0
	}
	return 2.0 * float64(matchCount(a, b)) / float64(total)
}

func isWordMatch(originalWord, censorWord string, minSimilarity float64) bool {
	normOriginal := normalizeText(originalWord)
	normCensor := normalizeText(censorWord)

	if normOriginal == normCensor {
		return true
	}

	if absInt(len([]rune(normOriginal)), len([]rune(normCensor))) > 2 {
		return false
	}

	return calculateSimilarity(normOriginal, normCensor) >= minSimilarity
}

func censorMatch(word string) string {
	runes := []rune(word)
	if len(runes) <= 2 {
		return strings.Repeat(string(CENSOR_CHAR), len(runes))
	}
	var sb strings.Builder
	sb.WriteRune(runes[0])
	for i := 1; i < len(runes); i++ {
		sb.WriteRune(CENSOR_CHAR)
	}
	return sb.String()
}

// replaceCaseInsensitiveAll replaces every case-insensitive occurrence of word,
// preserving the case pattern of the matched text (like re.sub with IGNORECASE).
func replaceCaseInsensitiveAll(s, word string) string {
	lowerS := strings.ToLower(s)
	lowerWord := strings.ToLower(word)
	var sb strings.Builder
	i := 0
	for i < len(s) {
		idx := strings.Index(lowerS[i:], lowerWord)
		if idx < 0 {
			sb.WriteString(s[i:])
			break
		}
		idx += i
		sb.WriteString(s[i:idx])
		matched := s[idx : idx+len(word)]
		sb.WriteString(censorMatch(matched))
		i = idx + len(word)
	}
	return sb.String()
}

func censorMessage(message string, censorList []string, minSimilarity float64) string {
	if censorList == nil {
		censorList = CENSOR_WORDS
	}
	if minSimilarity == 0 {
		minSimilarity = 0.85
	}
	words := findWords(message)
	for _, censorWord := range censorList {
		for _, originalWord := range words {
			if isWordMatch(originalWord, censorWord, minSimilarity) {
				message = replaceCaseInsensitiveAll(message, originalWord)
			}
		}
	}
	return message
}
