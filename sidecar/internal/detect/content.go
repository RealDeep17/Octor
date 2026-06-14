package detect

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	rxAlphaNumOnly   = regexp.MustCompile(`[^a-z0-9]`)
	rxExplicitXXX    = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])xxx(?:[\s._\-\]\)]|$)`)
	rxDotUnderscores = regexp.MustCompile(`[._]+`)
	rxBareYear       = regexp.MustCompile(`^(19|20)\d{2}$`)
	rxExtension      = regexp.MustCompile(`(?i)\.(mp4|mkv|avi|mov|wmv|webm|ts)$`)

	rxDate = regexp.MustCompile(`\b(?:(?:19|20)\d{2}[.\-_ ]+(?:0[1-9]|1[0-2])[.\-_ ]+(?:0[1-9]|[12]\d|3[01])|(?:0[1-9]|[12]\d|3[01])[.\-_ ]+(?:0[1-9]|1[0-2])[.\-_ ]+(?:19|20)\d{2}|\d{2}[.\-_ ]+(?:0[1-9]|1[0-2])[.\-_ ]+(?:0[1-9]|[12]\d|3[01]))\b`)

	// YYMMDD / YYMMDD date patterns common in adult torrents (e.g. "26 02 26" = 2026-02-26)
	rxYYMMDD = regexp.MustCompile(`(?:^|\s)(\d{2})[ .\-](\d{2})[ .\-](\d{2})(?:\s|$)`)

	rxCodecTags = regexp.MustCompile(`(?i)\b(bluray|blu-ray|webrip|web-dl|webdl|hdtv|dvdrip|bdrip|hdrip|camrip|1080p|720p|2160p|480p|4k|hevc|x264|x265|xvid|avc|remux|repack|proper)\b`)

	// Site-dash pattern: "SiteName - ..." where SiteName is a single alphanumeric word
	rxSiteDash = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{3,30})\s+-\s+`)

	rxJavCode      = regexp.MustCompile(`(?i)(?:^|[^a-zA-Z0-9])([A-Z]{2,6})[-_ ]?(\d{2,5})(?:[^a-zA-Z0-9]|$)`)
	rxJavDate      = regexp.MustCompile(`(?i)(?:^|[\s._\-])?(1pondo|caribbeancom|caribbean|10musume|heyzo|pacopacomama)[-_.]?(\d{6})[-_](\d{2,3})`)
	rxJavDateCheck = regexp.MustCompile(`^\d{2,4}[-._]\d{2}[-._]\d{2}`)
	rxSeasonTag    = regexp.MustCompile(`(?i)^S\d{1,2}$`)

	// Words that disqualify a site-dash match (too generic / not adult-specific)
	nonAdultPrefixes = map[string]struct{}{
		"the": {}, "a": {}, "an": {}, "my": {}, "your": {}, "our": {}, "his": {}, "her": {},
		"bts": {}, "kpop": {}, "anime": {}, "season": {}, "episode": {}, "vol": {},
		"coldplay": {}, "marvel": {}, "dc": {},
	}
)

var notJavPrefixes = map[string]struct{}{
	"hd":   {},
	"mp4":  {},
	"mkv":  {},
	"avi":  {},
	"ts":   {},
	"web":  {},
	"dl":   {},
	"blu":  {},
	"ray":  {},
	"uhd":  {},
	"hevc": {},
	"avc":  {},
	"hdr":  {},
	"sdr":  {},
	"dvd":  {},
	"bd":   {},
	"ep":   {},
	"s0":   {},
	"s1":   {},
	"s2":   {},
	"s3":   {},
	"scene":  {},
	"part":   {},
	"vol":    {},
	"volume": {},
}

func StudioInTitle(title string) bool {
	titleNoSep := rxAlphaNumOnly.ReplaceAllString(strings.ToLower(title), "")
	for s := range NSFWStudios {
		if strings.Contains(titleNoSep, s) {
			return true
		}
	}
	return false
}

func IsAdultContent(title string) bool {
	if StudioInTitle(title) {
		return true
	}

	if rxExplicitXXX.MatchString(title) {
		return true
	}

	stem := rxExtension.ReplaceAllString(title, "")
	stemCleaned := rxDotUnderscores.ReplaceAllString(stem, " ")

	// Date pattern (full YYYY-MM-DD style)
	if idxs := rxDate.FindStringIndex(stemCleaned); idxs != nil {
		before := strings.TrimSpace(stemCleaned[:idxs[0]])
		after := strings.TrimSpace(stemCleaned[idxs[1]:])
		if before != "" && after != "" {
			words := strings.Fields(before)
			lastWord := words[len(words)-1]
			if !rxBareYear.MatchString(lastWord) && !rxCodecTags.MatchString(before) {
				return true
			}
		}
	}

	// YYMMDD date pattern: "26 02 26" style common in adult torrents
	// Only trigger if there's content before AND after the date
	if m := rxYYMMDD.FindStringSubmatchIndex(stemCleaned); m != nil {
		before := strings.TrimSpace(stemCleaned[:m[0]])
		after := strings.TrimSpace(stemCleaned[m[1]:])
		if before != "" && after != "" && !rxCodecTags.MatchString(before) {
			return true
		}
	}

	// Site-dash pattern: "SiteName - Content" where SiteName is a short word.
	// This catches unknown studios without needing the static list.
	// e.g. "ThaiSwinger - BargirlPOV - ..." or "PornBox - Lily Phillips - ..."
	if m := rxSiteDash.FindStringSubmatch(title); len(m) > 1 {
		prefix := strings.ToLower(m[1])
		if _, isNonAdult := nonAdultPrefixes[prefix]; !isNonAdult {
			// Also require that rest of the title contains codec/quality tags
			// OR has another " - " (indicating multi-part adult naming convention)
			rest := title[len(m[0]):]
			if rxCodecTags.MatchString(rest) || strings.Contains(rest, " - ") {
				return true
			}
		}
	}

	return false
}


func ExtractJAVCode(title string) (string, bool) {
	stem := rxExtension.ReplaceAllString(title, "")

	// 1. Date-based sites first
	if matches := rxJavDate.FindStringSubmatch(stem); matches != nil {
		return strings.ToLower(matches[1]) + "-" + matches[2] + "_" + matches[3], true
	}

	// 2. Standard letter-number code
	if matches := rxJavCode.FindAllStringSubmatchIndex(stem, -1); matches != nil {
		for _, idxs := range matches {
			prefix := strings.ToUpper(stem[idxs[2]:idxs[3]])
			number := stem[idxs[4]:idxs[5]]

			remaining := stem[idxs[4]:]
			if rxJavDateCheck.MatchString(remaining) {
				continue
			}

			prefixLower := strings.ToLower(prefix)
			if prefixLower == "vr" && (number == "180" || number == "360") {
				continue
			}
			if _, isNotJav := notJavPrefixes[prefixLower]; isNotJav {
				continue
			}
			if _, isNsfw := NSFWStudios[prefixLower]; isNsfw {
				continue
			}
			if rxSeasonTag.MatchString(prefix) {
				continue
			}
			if len(number) == 4 {
				if numVal, err := strconv.Atoi(number); err == nil && numVal >= 1980 && numVal <= 2035 {
					continue
				}
			}

			paddedNumber := number
			if len(number) < 3 {
				paddedNumber = strings.Repeat("0", 3-len(number)) + number
			}

			return prefix + "-" + paddedNumber, true
		}
	}

	return "", false
}

func IsJAV(title string) bool {
	_, ok := ExtractJAVCode(title)
	return ok
}


