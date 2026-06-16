package parse

import (
	"regexp"
	"strings"

	"github.com/webtor-io/sidecar/internal/detect"
)

type ParsedFilename struct {
	Site      string
	Date      string
	Name      string
	Performer string
	Raw       string
}

var (
	rxCleanup = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(XXX|360p|480p|540p|576p|720p|1080p|1440p|2160p|4[Kk]|WEB[-. ]?DL|WEBRip|HDRip|BluRay|x264|x265|H\.?264|H\.?265|MP4|WRB|XC|SPLIT[-. ]?SCENES?|BTS|KTR|NBQ|BTM|WR|mkv|mp4|avi|wmv|mov|rq|p2p|ppv|vsex)\b`),
		regexp.MustCompile(`(?i)\b(PROPER|REPACK|READNFO|INTERNAL|LIMITED)\b`),
		regexp.MustCompile(`\[.*?\]`),
		regexp.MustCompile(`\(.*?\)`),
	}
	rxSeparators       = regexp.MustCompile(`[-_.]+`)
	rxWhitespace       = regexp.MustCompile(`\s+`)
	rxTrimHyphen       = regexp.MustCompile(`^[- ]+|[- ]+$`)
	rxExtension        = regexp.MustCompile(`(?i)\.(mp4|mkv|avi|mov|wmv|webm|ts)$`)
	rxHyphenSplit      = regexp.MustCompile(`\s+-\s+`)
	rxLeadingSep       = regexp.MustCompile(`^[-\s._()]+`)
	rxTrailingSep      = regexp.MustCompile(`[-\s._()]+$`)
	rxSingleLetterSite = regexp.MustCompile(`(?i)^([bt])([.\-_ ]+)`)
)

func NameCleaner(name string) string {
	for _, rx := range rxCleanup {
		name = rx.ReplaceAllString(name, "")
	}
	name = rxSeparators.ReplaceAllString(name, " ")
	name = rxWhitespace.ReplaceAllString(name, " ")
	name = rxTrimHyphen.ReplaceAllString(name, "")
	return name
}

func ParseAdultFilename(title string) ParsedFilename {
	stem := rxExtension.ReplaceAllString(title, "")

	parenthesizedStudio := ""
	rxParenthesesMatch := regexp.MustCompile(`\((.*?)\)`)
	pMatches := rxParenthesesMatch.FindAllStringSubmatch(stem, -1)
	for _, m := range pMatches {
		if len(m) > 1 {
			content := m[1]
			if studioKey, _, ok := detect.ExtractStudio(content); ok {
				parenthesizedStudio = studioKey
				break
			}
		}
	}

	result := ParsedFilename{
		Raw: title,
	}



	var stemNoDate string
	if dateStr, dStart, dEnd, ok := ExtractDate(stem); ok {
		result.Date = dateStr
		stemNoDate = stem[:dStart] + " " + stem[dEnd:]
	} else {
		stemNoDate = stem
	}

	// Extract single-letter studio prefixes (e.g. b. -> Blacked, t. -> Tushy)
	// ONLY if a valid date is present (i.e. dated scene releases)
	if result.Date != "" {
		if match := rxSingleLetterSite.FindStringSubmatch(stem); match != nil {
			siteLetter := strings.ToLower(match[1])
			if siteLetter == "b" {
				result.Site = "Blacked"
			} else if siteLetter == "t" {
				result.Site = "Tushy"
			}
			// Strip abbreviation from stemNoDate since it was derived from stem
			matchLen := len(match[0])
			if len(stemNoDate) >= matchLen && strings.EqualFold(stemNoDate[:matchLen], match[0]) {
				stemNoDate = stemNoDate[matchLen:]
			}
		}
	}

	parts := rxHyphenSplit.Split(stemNoDate, -1)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	if len(parts) >= 3 {
		result.Site = strings.TrimSpace(NameCleaner(parts[0]))
		result.Performer = strings.TrimSpace(NameCleaner(parts[1]))
		result.Name = strings.TrimSpace(NameCleaner(strings.Join(parts[2:], " - ")))
	} else if len(parts) == 2 {
		result.Site = strings.TrimSpace(NameCleaner(parts[0]))
		cleanedRight := strings.TrimSpace(NameCleaner(parts[1]))
		if len(strings.Fields(cleanedRight)) <= 3 {
			result.Performer = cleanedRight
			result.Name = cleanedRight
		} else {
			result.Name = cleanedRight
		}
	} else {
		if _, origSite, ok := detect.ExtractStudio(stemNoDate); ok {
			result.Site = strings.Trim(origSite, " -._")
			stemNoDate = strings.ReplaceAll(stemNoDate, origSite, " ")
		}
		cleanedName := NameCleaner(stemNoDate)
		cleanedName = rxLeadingSep.ReplaceAllString(cleanedName, "")
		cleanedName = rxTrailingSep.ReplaceAllString(cleanedName, "")
		result.Name = strings.TrimSpace(cleanedName)
	}

	if result.Performer == "" && result.Name != "" && len(strings.Fields(result.Name)) <= 3 {
		result.Performer = result.Name
	}

	if result.Site == "" && parenthesizedStudio != "" {
		result.Site = parenthesizedStudio
	}

	if result.Site != "" {
		siteLower := strings.ToLower(strings.TrimSpace(result.Site))
		if mapped, exists := detect.StudioAbbreviations[siteLower]; exists {
			result.Site = mapped
		}
	}

	return result
}
