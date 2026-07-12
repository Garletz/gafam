package suparna

import (
	"regexp"
	"sort"
	"strings"
)

var (
	codeKeywordRe = regexp.MustCompile(`(?i)(code|otp|pin|verification|confirm|mot de passe|mot-de-passe|accès|acces|gafam|impulsion)`)
	// Standalone numeric codes (4–8 digits), optional spaced groups.
	numericCodeRe = regexp.MustCompile(`(?:^|[\s:：\-—])((?:\d[\s\-]?){4,8}\d)(?:[\s.,!?]|$)`)
	// HH:MM style recovery codes
	timeCodeRe = regexp.MustCompile(`\b([01]?\d|2[0-3])[:h]([0-5]\d)\b`)
	// Alphanumeric OTP (e.g. A1B2C3)
	alphaNumCodeRe = regexp.MustCompile(`\b([A-Z0-9]{4,8})\b`)
)

// DetectCodes finds likely verification / OTP codes in SMS or log text.
func DetectCodes(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string

	add := func(raw string) {
		c := normalizeCode(raw)
		if c == "" || len(c) < 4 || len(c) > 10 {
			return
		}
		// Skip obvious phone fragments / years
		if len(c) == 4 && (c >= "1900" && c <= "2099") {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}

	lower := strings.ToLower(text)
	hasKeyword := codeKeywordRe.MatchString(lower)

	if m := timeCodeRe.FindAllStringSubmatch(text, -1); len(m) > 0 && hasKeyword {
		for _, g := range m {
			if len(g) >= 3 {
				add(g[1] + g[2])
				add(g[1] + ":" + g[2])
			}
		}
	}

	for _, m := range numericCodeRe.FindAllStringSubmatch(text, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}

	if hasKeyword {
		for _, m := range alphaNumCodeRe.FindAllStringSubmatch(strings.ToUpper(text), -1) {
			if len(m) > 1 && !isCommonWord(m[1]) {
				add(m[1])
			}
		}
	}

	sort.Strings(out)
	return out
}

func normalizeCode(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.ReplaceAll(raw, "-", "")
	raw = strings.ToUpper(raw)
	return raw
}

func isCommonWord(s string) bool {
	switch s {
	case "GAFAM", "CODE", "HTTP", "HTTPS", "TRUE", "FALSE", "NULL", "SMS", "AUTH":
		return true
	}
	return false
}
