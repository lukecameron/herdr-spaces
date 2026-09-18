package naming

import (
	"strings"
	"unicode"
)

const (
	maxWords = 5
	maxRunes = 40
)

// Sanitize turns the model's reply into a label, or reports that the reply
// is not usable. It keeps the first non-empty line, strips quotes and
// trailing punctuation, and caps the length.
func Sanitize(raw string) (string, bool) {
	var line string
	for _, l := range strings.Split(raw, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	if line == "" {
		return "", false
	}

	line = strings.Trim(line, "\"'`“”‘’*_")
	line = strings.TrimRightFunc(line, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
	line = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, line)

	words := strings.Fields(line)
	if len(words) == 0 {
		return "", false
	}
	if len(words) > maxWords {
		words = words[:maxWords]
	}
	label := strings.Join(words, " ")
	if runes := []rune(label); len(runes) > maxRunes {
		label = strings.TrimSpace(string(runes[:maxRunes]))
	}
	if strings.Contains(strings.ToLower(label), "label:") || strings.HasPrefix(strings.ToLower(label), "i ") {
		return "", false
	}
	return label, true
}
