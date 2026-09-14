package board

import (
	"regexp"
	"strconv"
)

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

var (
	reGoTotal         = regexp.MustCompile(`total:\s+\(statements\)\s+([0-9.]+)%`)
	reGoPerPackage    = regexp.MustCompile(`coverage:\s+([0-9.]+)% of statements`)
	reJestLines       = regexp.MustCompile(`Lines\s*:\s*([0-9.]+)%`)
	reJestTable       = regexp.MustCompile(`All files[^|]*\|\s*[0-9.]+\s*\|\s*[0-9.]+\s*\|\s*[0-9.]+\s*\|\s*([0-9.]+)`)
	rePytestCov       = regexp.MustCompile(`TOTAL\s+\d+\s+\d+\s+([0-9.]+)%`)
	reGenericFallback = regexp.MustCompile(`(?i)(?:total|overall)[^%\n]*?([0-9.]{1,6})%`)
)

func ParseCoverage(log string) *float64 {
	clean := ansiEscapeRe.ReplaceAllString(log, "")

	if v, ok := firstMatch(reGoTotal, clean, 1); ok {
		return validPercent(v)
	}
	if v, ok := meanMatch(reGoPerPackage, clean, 1); ok {
		return validPercent(v)
	}
	if v, ok := firstMatch(reJestLines, clean, 1); ok {
		return validPercent(v)
	}
	if v, ok := firstMatch(reJestTable, clean, 1); ok {
		return validPercent(v)
	}
	if v, ok := firstMatch(rePytestCov, clean, 1); ok {
		return validPercent(v)
	}
	if v, ok := firstValidGenericMatch(clean); ok {
		return validPercent(v)
	}
	return nil
}

func firstMatch(re *regexp.Regexp, s string, group int) (float64, bool) {
	m := re.FindStringSubmatch(s)
	if m == nil || group >= len(m) {
		return 0, false
	}
	f, err := strconv.ParseFloat(m[group], 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func meanMatch(re *regexp.Regexp, s string, group int) (float64, bool) {
	matches := re.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return 0, false
	}
	var sum float64
	var count int
	for _, m := range matches {
		if group >= len(m) {
			continue
		}
		f, err := strconv.ParseFloat(m[group], 64)
		if err != nil {
			continue
		}
		sum += f
		count++
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
}

func firstValidGenericMatch(s string) (float64, bool) {
	for _, m := range reGenericFallback.FindAllStringSubmatch(s, -1) {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil || f > 100 {
			continue
		}
		return f, true
	}
	return 0, false
}

func validPercent(v float64) *float64 {
	if v < 0 || v > 100 {
		return nil
	}
	return &v
}
