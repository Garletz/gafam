package main

import (
	"regexp"
	"sort"
	"strings"
)

var (
	codeKeywordRe  = regexp.MustCompile(`(?i)(code|otp|pin|verification|confirm|mot de passe|mot-de-passe|accès|acces|gafam|impulsion)`)
	numericCodeRe  = regexp.MustCompile(`(?:^|[\s:：\-—])((?:\d[\s\-]?){4,8}\d)(?:[\s.,!?]|$)`)
	timeCodeRe     = regexp.MustCompile(`\b([01]?\d|2[0-3])[:h]([0-5]\d)\b`)
	alphaNumCodeRe = regexp.MustCompile(`\b([A-Z0-9]{4,8})\b`)
)

// detectSmsCodes finds likely verification / OTP codes in SMS or log text.
func detectSmsCodes(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string

	add := func(raw string) {
		c := normalizeSmsCode(raw)
		if c == "" || len(c) < 4 || len(c) > 10 {
			return
		}
		if len(c) == 4 && c >= "1900" && c <= "2099" {
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
			if len(m) > 1 && !isCommonSmsWord(m[1]) {
				add(m[1])
			}
		}
	}

	sort.Strings(out)
	return out
}

func normalizeSmsCode(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.ReplaceAll(raw, "-", "")
	raw = strings.ToUpper(raw)
	return raw
}

func isCommonSmsWord(s string) bool {
	switch s {
	case "GAFAM", "CODE", "HTTP", "HTTPS", "TRUE", "FALSE", "NULL", "SMS", "AUTH":
		return true
	}
	return false
}
