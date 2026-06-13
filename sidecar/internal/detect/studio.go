package detect

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/mozillazg/go-unidecode"
)

type studioPattern struct {
	Name string
	Rx   *regexp.Regexp
}

var studioPatterns []studioPattern

func init() {
	studioPatterns = make([]studioPattern, 0, len(NSFWStudios))
	for studio := range NSFWStudios {
		var parts []string
		for _, r := range studio {
			parts = append(parts, regexp.QuoteMeta(string(r)))
		}
		var patternStr string
		for i, p := range parts {
			if i > 0 {
				patternStr += `[.\-_ ]*`
			}
			patternStr += p
		}
		rx := regexp.MustCompile(`(?i)` + patternStr)
		studioPatterns = append(studioPatterns, studioPattern{
			Name: studio,
			Rx:   rx,
		})
	}
}

type studioMatch struct {
	start      int
	keyLength  int
	studioKey  string
	matchedSub string
}

func ExtractStudio(text string) (studioKey, matchedSub string, ok bool) {
	var matches []studioMatch
	for _, sp := range studioPatterns {
		if idxs := sp.Rx.FindStringIndex(text); idxs != nil {
			matches = append(matches, studioMatch{
				start:      idxs[0],
				keyLength:  len(sp.Name),
				studioKey:  sp.Name,
				matchedSub: text[idxs[0]:idxs[1]],
			})
		}
	}

	if len(matches) == 0 {
		return "", "", false
	}

	// Sort matches:
	// 1. By start index ascending (earliest match wins)
	// 2. By length of studio key descending (longer match wins for same start index)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].start != matches[j].start {
			return matches[i].start < matches[j].start
		}
		return matches[i].keyLength > matches[j].keyLength
	})

	best := matches[0]
	return best.studioKey, best.matchedSub, true
}

func IsSubsequence(sub, str string) bool {
	if len(sub) == 0 {
		return true
	}
	subIdx := 0
	for i := 0; i < len(str); i++ {
		if str[i] == sub[subIdx] {
			subIdx++
			if subIdx == len(sub) {
				return true
			}
		}
	}
	return false
}

var rxWordLetters = regexp.MustCompile(`[a-z0-9]+`)

func IsAbbreviation(abbrev, fullName string) bool {
	if abbrev == "" || fullName == "" {
		return false
	}
	abbrev = strings.ToLower(strings.TrimSpace(abbrev))
	if abbrev == "" || len(abbrev) > 6 {
		return false
	}

	fullNameClean := strings.ToLower(unidecode.Unidecode(fullName))
	fullNameNoSep := rxNotAlphanumeric.ReplaceAllString(fullNameClean, "")

	// 1. Check direct map
	if mapped, exists := StudioAbbreviations[abbrev]; exists {
		mappedClean := rxNotAlphanumeric.ReplaceAllString(strings.ToLower(mapped), "")
		if strings.Contains(mappedClean, fullNameNoSep) || strings.Contains(fullNameNoSep, mappedClean) {
			return true
		}
	}

	// Split by spaces and separators
	words := rxWordLetters.FindAllString(fullNameClean, -1)
	if len(words) == 0 {
		return false
	}

	// Heuristic 1: First letter of each word
	var firstLettersBuilder strings.Builder
	for _, w := range words {
		if len(w) > 0 {
			firstLettersBuilder.WriteByte(w[0])
		}
	}
	firstLetters := firstLettersBuilder.String()
	if abbrev == firstLetters {
		return true
	}

	// Heuristic 2: Capital letters in a camelCase/PascalCase string
	var capsBuilder strings.Builder
	for _, r := range fullName {
		if unicode.IsUpper(r) {
			capsBuilder.WriteRune(unicode.ToLower(r))
		}
	}
	caps := capsBuilder.String()
	if caps != "" && strings.Contains(caps, abbrev) {
		return true
	}

	// Heuristic 3: Substring of first letters
	if len(abbrev) >= 2 && strings.Contains(firstLetters, abbrev) {
		return true
	}

	// Heuristic 4: Subsequence matching for abbreviations >= 3 characters
	if len(abbrev) >= 3 && IsSubsequence(abbrev, fullNameNoSep) {
		return true
	}

	return false
}

var rxNotAlphanumeric = regexp.MustCompile(`[^a-z0-9]`)
