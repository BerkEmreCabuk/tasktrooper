package taskkey

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var keyPrefixPattern = regexp.MustCompile(`^[A-Z]{2,5}$`)

func ValidateKeyPrefix(prefix string) error {
	p := strings.ToUpper(strings.TrimSpace(prefix))
	if !keyPrefixPattern.MatchString(p) {
		return fmt.Errorf("board key must be 2-5 uppercase letters")
	}
	return nil
}

func NormalizeKeyPrefix(prefix string) string {
	return strings.ToUpper(strings.TrimSpace(prefix))
}

func SuggestKeyPrefix(name string) string {
	var letters []rune
	for _, r := range name {
		if unicode.IsLetter(r) {
			letters = append(letters, unicode.ToUpper(r))
		}
	}
	if len(letters) >= 2 {
		if len(letters) > 5 {
			letters = letters[:5]
		}
		return string(letters)
	}
	if len(letters) == 1 {
		return string(letters) + "T"
	}
	return "TM"
}

func FormatTaskKey(keyPrefix string, number int) string {
	return fmt.Sprintf("%s-%d", NormalizeKeyPrefix(keyPrefix), number)
}
