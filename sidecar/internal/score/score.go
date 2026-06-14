package score

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mozillazg/go-unidecode"
	"github.com/webtor-io/sidecar/internal/detect"
	"github.com/webtor-io/sidecar/internal/fuzzy"
	"github.com/webtor-io/sidecar/internal/parse"
	"github.com/webtor-io/sidecar/internal/scene"

	fuzzywuzzy "github.com/paul-mannino/go-fuzzywuzzy"
)

var (
	rxAlphanumeric  = regexp.MustCompile(`[^a-z0-9]`)
	rxColonOrHyphen = regexp.MustCompile(`[:\-]`)
	rxParentheses   = regexp.MustCompile(`\(.*?\)`)
	rxWord          = regexp.MustCompile(`[a-z0-9]+`)
	rxWord3         = regexp.MustCompile(`[a-z0-9]{3,}`)
	rxBareYear      = regexp.MustCompile(`^(19|20)\d{2}$`)
	rxTrailingNum   = regexp.MustCompile(`(?i)(?:(?:part|vol|volume|ep|episode|visit|#)\s*([0-9]+)|\b([0-9]+)\s*$)`)
	// Trailer/preview/BTS indicator keywords in scene titles
	trailerKeywords = []string{"trailer", "preview", "teaser", "promo", "bts", "behind the scenes", "behind-the-scenes", "making of", "making-of"}
	// Compilation/best-of indicator keywords in scene titles
	compilationKeywords = []string{"compilation", "best of", "best of ", "collection", "greatest hits", "cumpilation", "cumshot compilation"}
)

func cleanAlphanumeric(s string) string {
	return rxAlphanumeric.ReplaceAllString(strings.ToLower(s), "")
}

func splitColonOrHyphen(s string) []string {
	return rxColonOrHyphen.Split(s, -1)
}

func stripParentheses(s string) string {
	return rxParentheses.ReplaceAllString(s, "")
}

func getWords(s string) []string {
	return rxWord.FindAllString(strings.ToLower(s), -1)
}

func getWords3(s string) []string {
	return rxWord3.FindAllString(strings.ToLower(s), -1)
}

// extractSeriesNum extracts a trailing series/part/volume/visit number from a title.
// Matches patterns like "Part 3", "Vol 2", "Visit 4", "15" at end of string.
// Returns 0 if no such number found.
func extractSeriesNum(s string) int {
	m := rxTrailingNum.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return 0
	}
	// group 1: named keyword match (part/vol/...), group 2: bare trailing number
	for _, g := range m[1:] {
		if g != "" {
			n := 0
			for _, c := range g {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			if n > 0 && n < 500 { // sanity cap (avoid matching years)
				return n
			}
		}
	}
	return 0
}

var stopWordsAndCodecs = map[string]struct{}{
	"ly": {}, "rq": {}, "mp4": {}, "mkv": {}, "avi": {}, "wmv": {}, "ts": {},
}

var stopWords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "you": {}, "your": {}, "that": {}, "this": {}, "from": {}, "her": {}, "him": {}, "she": {}, "his": {}, "out": {}, "our": {}, "all": {}, "its": {}, "under": {},
	"1080p": {}, "2160p": {}, "4096p": {}, "4k": {}, "8k": {}, "hd": {}, "fhd": {}, "sd": {}, "mp4": {}, "mkv": {}, "avi": {}, "wmv": {}, "mov": {}, "webm": {}, "ts": {}, "vr180": {}, "xxx": {}, "hevc": {}, "x264": {}, "x265": {}, "h264": {}, "h265": {},
	"360p": {}, "480p": {}, "540p": {}, "576p": {}, "1440p": {}, "ktr": {}, "nbq": {}, "btm": {}, "wr": {}, "rq": {},
	"p2p": {}, "xc": {}, "wrb": {}, "vsex": {}, "rarbg": {}, "yify": {}, "eztv": {}, "fgt": {}, "narcos": {}, "prt": {}, "vol": {}, "ch": {}, "ppv": {},
	"had": {}, "has": {}, "have": {}, "was": {}, "were": {}, "are": {}, "is": {}, "get": {}, "gets": {}, "got": {}, "take": {}, "takes": {}, "took": {}, "give": {}, "gives": {}, "gave": {}, "make": {}, "makes": {}, "made": {}, "come": {}, "comes": {}, "came": {}, "go": {}, "goes": {}, "went": {}, "do": {}, "does": {}, "did": {}, "new": {}, "old": {}, "big": {}, "small": {}, "one": {}, "two": {}, "three": {}, "first": {}, "last": {}, "just": {}, "about": {}, "some": {}, "like": {}, "how": {}, "why": {}, "who": {}, "what": {}, "where": {}, "when": {}, "can": {}, "could": {}, "would": {}, "should": {}, "will": {}, "shall": {}, "may": {}, "might": {}, "must": {}, "but": {}, "not": {}, "too": {}, "very": {}, "much": {}, "many": {}, "more": {}, "most": {}, "few": {}, "less": {}, "least": {}, "own": {}, "other": {}, "same": {}, "different": {}, "good": {}, "bad": {}, "hot": {}, "cool": {}, "warm": {}, "cold": {}, "now": {}, "then": {}, "once": {}, "twice": {}, "here": {}, "there": {}, "every": {}, "each": {}, "both": {}, "either": {}, "neither": {}, "any": {}, "none": {}, "only": {}, "well": {}, "done": {},
}

var genericKeywords = map[string]struct{}{
	"fuck": {}, "fucks": {}, "fucked": {}, "fucking": {}, "suck": {}, "sucks": {}, "sucked": {}, "sucking": {},
	"blowjob": {}, "bj": {}, "bjs": {}, "bj's": {}, "anal": {}, "creampie": {}, "cum": {}, "cums": {}, "cumming": {}, "swallow": {},
	"swallows": {}, "swallowed": {}, "ride": {}, "rides": {}, "riding": {}, "facial": {}, "facials": {},
	"fist": {}, "fisting": {}, "peg": {}, "pegging": {}, "strip": {}, "strips": {}, "stripping": {},
	"masturbate": {}, "masturbating": {}, "jerk": {}, "jerking": {}, "squirt": {}, "squirting": {},
	"lick": {}, "licks": {}, "licking": {}, "fuckboys": {}, "fuckboy": {}, "cuck": {}, "cuckold": {}, "cucks": {},
	"cute": {}, "hot": {}, "sexy": {}, "gorgeous": {}, "beautiful": {}, "pretty": {}, "busty": {}, "petite": {},
	"blonde": {}, "brunette": {}, "ebony": {}, "asian": {}, "latina": {}, "teen": {}, "milf": {}, "milfs": {},
	"new": {}, "old": {}, "first": {}, "last": {}, "good": {}, "bad": {}, "big": {}, "small": {}, "hard": {}, "soft": {},
	"raw": {}, "real": {}, "fake": {}, "dirty": {}, "clean": {}, "bored": {}, "lazy": {}, "natural": {}, "wild": {},
	"step": {}, "stepmom": {}, "stepsis": {}, "sister": {}, "brother": {}, "stepbrother": {}, "dad": {}, "mom": {},
	"stepdaughter": {}, "stepson": {}, "stepsister": {}, "daddy": {}, "mommy": {}, "wife": {}, "husband": {},
	"girlfriend": {}, "boyfriend": {}, "friend": {}, "friends": {}, "roommate": {}, "roommates": {},
	"landlord": {}, "boss": {}, "maid": {}, "nurse": {}, "barista": {}, "girl": {}, "girls": {}, "guy": {}, "guys": {},
	"video": {}, "videos": {}, "scene": {}, "scenes": {}, "drop": {}, "drops": {}, "trailer": {}, "trailers": {},
	"homemade": {}, "show": {}, "shows": {}, "clip": {}, "clips": {}, "preview": {}, "previews": {},
	"teaser": {}, "teasers": {}, "brand": {}, "part": {}, "episode": {}, "vol": {}, "volume": {}, "ch": {}, "chapter": {},
	"exclusive": {}, "exclusives": {}, "special": {}, "specials": {}, "anniversary": {}, "update": {},
	"orgy": {}, "threesome": {}, "dp": {}, "double": {}, "penetration": {}, "mmf": {}, "ffm": {},
	"sextape": {}, "tape": {}, "tapes": {}, "pov": {}, "bts": {}, "behind": {}, "cast": {}, "casting": {},
	"couch": {}, "audition": {}, "auditions": {}, "interview": {}, "interviews": {}, "live": {}, "stream": {},
	"livestream": {}, "footage": {}, "hauls": {}, "haul": {},
	"night": {}, "day": {}, "morning": {}, "afternoon": {}, "evening": {}, "today": {}, "yesterday": {},
}

var genericPlatforms = map[string]struct{}{
	"onlyfans": {}, "fansly": {}, "manyvids": {}, "fansdb": {}, "patreon": {}, "fans": {},
	"xvideos": {}, "pornhub": {}, "spankbang": {}, "redtube": {}, "tube": {},
}

func ScoreResult(parsed parse.ParsedFilename, scene scene.Scene, targetDuration *float64) float64 {
	score := 0.0

	// 1. Dynamic Site Recognition (effectiveSite is local to avoid data races)
	effectiveSite := parsed.Site
	if effectiveSite == "" {
		rawClean := cleanAlphanumeric(unidecode.Unidecode(parsed.Raw))
		for _, val := range []string{scene.Site, scene.Parent, scene.Network} {
			if val != "" {
				valClean := cleanAlphanumeric(unidecode.Unidecode(val))
				if len(valClean) >= 4 && strings.Contains(rawClean, valClean) {
					effectiveSite = val
					break
				}
			}
		}
	}

	// 2. Pre-calculate Platform and Performer Match
	parsedSiteClean := cleanAlphanumeric(unidecode.Unidecode(effectiveSite))

	_, parsedIsGeneric := genericPlatforms[parsedSiteClean]

	sceneSiteClean := cleanAlphanumeric(unidecode.Unidecode(scene.Site))
	sceneIsGeneric := false
	for p := range genericPlatforms {
		if sceneSiteClean == p || strings.HasPrefix(sceneSiteClean, p+":") {
			sceneIsGeneric = true
			break
		}
	}
	isPlatform := parsedIsGeneric || sceneIsGeneric

	perfMatch := false
	rawTitle := strings.ToLower(parsed.Raw)

	if parsed.Performer != "" {
		parsedPerfClean := cleanAlphanumeric(unidecode.Unidecode(parsed.Performer))
		for _, p := range scene.Performers {
			if p.Name != "" {
				nameClean := cleanAlphanumeric(unidecode.Unidecode(p.Name))
				if parsedPerfClean != "" && (strings.Contains(nameClean, parsedPerfClean) || strings.Contains(parsedPerfClean, nameClean) || fuzzy.Score(parsed.Performer, p.Name) >= 85.0) {
					perfMatch = true
					break
				}
			}
		}

		if !perfMatch && scene.Title != "" {
			titleClean := cleanAlphanumeric(unidecode.Unidecode(scene.Title))
			if len(parsedPerfClean) >= 4 && strings.Contains(titleClean, parsedPerfClean) {
				perfMatch = true
			}
		}
	}

	if !perfMatch {
		for _, p := range scene.Performers {
			if p.Name != "" {
				nameLower := strings.ToLower(p.Name)
				perfClean := cleanAlphanumeric(unidecode.Unidecode(nameLower))
				if parsedSiteClean != "" && (parsedSiteClean == perfClean || strings.Contains(parsedSiteClean, perfClean) || strings.Contains(perfClean, parsedSiteClean)) {
					continue
				}

				if strings.Contains(rawTitle, nameLower) {
					perfMatch = true
					break
				}
				cleanedPerf := cleanAlphanumeric(unidecode.Unidecode(nameLower))
				cleanedRaw := cleanAlphanumeric(unidecode.Unidecode(rawTitle))
				if strings.Contains(cleanedRaw, cleanedPerf) {
					perfMatch = true
					break
				}
			}
		}
	}

	isOnlyPerformer := false
	if parsed.Name != "" && perfMatch {
		allPerfWords := make(map[string]struct{})
		for _, p := range scene.Performers {
			if p.Name != "" {
				for _, w := range getWords(p.Name) {
					allPerfWords[w] = struct{}{}
				}
			}
		}
		qWords := getWords(parsed.Name)
		hasExtraWords := false
		for _, qw := range qWords {
			if _, ok := stopWords[qw]; ok {
				continue
			}
			if _, ok := genericKeywords[qw]; ok {
				hasExtraWords = true
				break
			}
			if _, ok := allPerfWords[qw]; ok {
				continue
			}
			hasExtraWords = true
			break
		}
		if !hasExtraWords {
			isOnlyPerformer = true
		}
	}

	// 3. Site match / mismatch penalty
	siteMatched := false
	if effectiveSite != "" {
		parsedSite := cleanAlphanumeric(unidecode.Unidecode(effectiveSite))
		_, isWhitelisted := detect.NSFWStudios[parsedSite]

		sceneSite := cleanAlphanumeric(unidecode.Unidecode(scene.Site))
		if parsedSite != "" && sceneSite != "" && (strings.Contains(sceneSite, parsedSite) || strings.Contains(parsedSite, sceneSite) || detect.IsAbbreviation(effectiveSite, scene.Site)) {
			if isWhitelisted {
				score += 120
			} else {
				score += 80
			}
			siteMatched = true
		} else {
			sceneSites := []string{
				cleanAlphanumeric(unidecode.Unidecode(scene.Parent)),
				cleanAlphanumeric(unidecode.Unidecode(scene.Network)),
			}
			for _, sName := range sceneSites {
				if sName != "" && (strings.Contains(sName, parsedSite) || strings.Contains(parsedSite, sName)) {
					siteMatched = true
					break
				}
			}

			if siteMatched {
				if isWhitelisted {
					score += 100
				} else {
					score += 60
				}
			} else {
				if !isPlatform || (isWhitelisted && sceneIsGeneric) {
					hasStrongPerformerAndDate := perfMatch && parsed.Date != "" && scene.Date != "" && strings.HasPrefix(scene.Date, parsed.Date)
					if isWhitelisted && !hasStrongPerformerAndDate {
						score -= 180
					} else {
						score -= 80
					}
				}
			}
		}
	} else {
		score += 50
	}

	// 3.2 Platform-compatible site matching
	if !siteMatched && isPlatform && parsedSiteClean != "" && effectiveSite != "" {
		sceneSiteRawLower := strings.ToLower(unidecode.Unidecode(scene.Site))
		if strings.Contains(sceneSiteRawLower, parsedSiteClean) {
			siteMatched = true
			score += 50
		}
	}

	// 3.3 For generic platforms (OnlyFans etc.), try to match creator name from scene.Site
	// e.g. scene.Site = "OnlyFans: Madiiitay" → extract "madiiitay" and check in raw filename.
	if isPlatform && !perfMatch && scene.Site != "" {
		parts := rxColonOrHyphen.Split(scene.Site, -1)
		if len(parts) >= 2 {
			creatorPart := strings.TrimSpace(stripParentheses(parts[1]))
			creatorClean := cleanAlphanumeric(unidecode.Unidecode(creatorPart))
			rawClean := cleanAlphanumeric(unidecode.Unidecode(parsed.Raw))
			if len(creatorClean) >= 4 && strings.Contains(rawClean, creatorClean) {
				// Creator name found in torrent filename — treat as a strong contextual match
				score += 80
			}
		}
	}

	// 3.5 Creator/Performer match validation for generic platforms
	if isPlatform && !perfMatch {
		hasCreatorInFilename := false
		if scene.Site != "" {
			parts := rxColonOrHyphen.Split(scene.Site, -1)
			if len(parts) >= 2 {
				creatorPart := stripParentheses(parts[1])
				creatorName := cleanAlphanumeric(unidecode.Unidecode(creatorPart))
				rawClean := cleanAlphanumeric(unidecode.Unidecode(parsed.Raw))
				if len(creatorName) >= 3 && strings.Contains(rawClean, creatorName) {
					hasCreatorInFilename = true
				}
			}
		}
		if !hasCreatorInFilename {
			score -= 300.0
		}
	}

	// 4. Date match / mismatch
	if parsed.Date != "" {
		if scene.Date != "" {
			sDateLen := len(scene.Date)
			if sDateLen > 10 {
				sDateLen = 10
			}
			pDate, err1 := time.Parse("2006-01-02", parsed.Date)
			sDate, err2 := time.Parse("2006-01-02", scene.Date[:sDateLen])
			if err1 == nil && err2 == nil {
				diffDays := int(math.Abs(pDate.Sub(sDate).Hours() / 24.0))
				if diffDays <= 1 {
					score += 100
					// Double-confirm bonus: date AND performer both match
					if perfMatch {
						score += 50
					}
				} else if diffDays <= 2 {
					score += 50
					if perfMatch {
						score += 25
					}
				} else if diffDays <= 7 {
					score += 20
				} else {
					// Date diverges more than 7 days — apply penalty scaled by context.
					// For series (same site+perf, date diverged): apply a stronger penalty
					// to prevent the wrong series part from winning.
					if perfMatch && siteMatched {
						if isOnlyPerformer {
							score -= 20
						} else {
							// Stronger penalty for series-part disambiguation:
							// e.g. Brazzers Part 1 vs Part 2 — date off by 1 day
							// but here date is off by >7 days, so this is likely wrong part
							score -= 70
						}
					} else {
						tScore := fuzzy.Score(parsed.Name, scene.Title)
						if tScore >= 85.0 && !isOnlyPerformer {
							score -= 50
						} else {
							score -= 100
						}
					}
				}
			} else {
				if parsed.Date == scene.Date[:sDateLen] {
					score += 100
					if perfMatch {
						score += 50
					}
				} else {
					if perfMatch && siteMatched {
						if isOnlyPerformer {
							score -= 20
						} else {
							score -= 70
						}
					} else {
						tScore := fuzzy.Score(parsed.Name, scene.Title)
						if tScore >= 85.0 && !isOnlyPerformer {
							score -= 50
						} else {
							score -= 100
						}
					}
				}
			}
		}
	} else {
		score += 50
	}

	// 5. Apply performer match boost + multi-performer bonus
	if perfMatch {
		score += 150
	}

	// 5b. Multi-performer bonus: when multiple scene performers appear in the raw torrent title,
	// this is a very strong signal — boost for each additional match beyond the first.
	// This helps distinguish between two candidates that both match a single performer name.
	if len(scene.Performers) >= 2 {
		multiPerfCount := 0
		rawLower := strings.ToLower(parsed.Raw)
		rawClean := cleanAlphanumeric(unidecode.Unidecode(rawLower))
		for _, p := range scene.Performers {
			if p.Name == "" {
				continue
			}
			nameLower := strings.ToLower(p.Name)
			nameClean := cleanAlphanumeric(unidecode.Unidecode(nameLower))
			if len(nameClean) >= 3 && strings.Contains(rawClean, nameClean) {
				multiPerfCount++
			} else if strings.Contains(rawLower, nameLower) {
				multiPerfCount++
			}
		}
		if multiPerfCount >= 2 {
			score += float64(multiPerfCount-1) * 60.0
		}
	}

	// 5c. Trailer/BTS scoring: penalise mismatch between torrent and scene on BTS/trailer type.
	if scene.Title != "" {
		sceneTitleLower := strings.ToLower(scene.Title)
		rawTorrentLower := strings.ToLower(parsed.Raw)
		sceneIsTrailer := false
		for _, kw := range trailerKeywords {
			if strings.Contains(sceneTitleLower, kw) {
				sceneIsTrailer = true
				break
			}
		}
		torrentHasTrailer := false
		for _, kw := range trailerKeywords {
			if strings.Contains(rawTorrentLower, kw) {
				torrentHasTrailer = true
				break
			}
		}
		if sceneIsTrailer && !torrentHasTrailer {
			// Scene is BTS/trailer but torrent is not — penalise strongly
			score -= 70.0
		} else if torrentHasTrailer && !sceneIsTrailer {
			// Torrent is BTS/trailer but matched scene isn't — softer penalty
			// (some BTS scenes don't include "BTS" in their title)
			score -= 50.0
		}

		// 5d. Compilation/best-of scoring: penalise compilation scenes when torrent is not.
		// This prevents year-end compilations from outranking individual episodes.
		sceneIsCompilation := false
		for _, kw := range compilationKeywords {
			if strings.Contains(sceneTitleLower, kw) {
				sceneIsCompilation = true
				break
			}
		}
		torrentHasCompilation := false
		for _, kw := range compilationKeywords {
			if strings.Contains(rawTorrentLower, kw) {
				torrentHasCompilation = true
				break
			}
		}
		if sceneIsCompilation && !torrentHasCompilation {
			// Scene is a compilation but torrent filename doesn't indicate one — penalise
			score -= 150.0
		}

		// 5e. Series/volume number mismatch: penalise when torrent specifies a part/vol number
		// that doesn't match the scene's part/vol number.
		// e.g. torrent "Granny Loves Cock 4" but scene "Granny Loves Cock" → wrong volume.
		// e.g. torrent "Sleezy Rider" but scene "Sleazy Rider: Part 3" → spurious part.
		if !isOnlyPerformer && parsed.Name != "" {
			torrentSeriesNum := extractSeriesNum(parsed.Name)
			sceneSeriesNum := extractSeriesNum(scene.Title)
			switch {
			case torrentSeriesNum > 0 && sceneSeriesNum > 0 && torrentSeriesNum != sceneSeriesNum:
				// Both have a number but they differ — strong mismatch signal
				score -= 80.0
			case torrentSeriesNum > 0 && sceneSeriesNum == 0 && siteMatched:
				// Torrent specifies a volume but scene is generic (no number) — likely wrong episode
				score -= 45
			case torrentSeriesNum == 0 && sceneSeriesNum > 0 && siteMatched:
				// Scene has a part number but torrent doesn't mention one — likely wrong part selected
				score -= 40
			}
		}
	}

	// 6. Name fuzzy match against scene title only
	if parsed.Name != "" && scene.Title != "" {
		titleLower := strings.ToLower(scene.Title)

		isPerfTitle := false
		for _, p := range scene.Performers {
			pName := p.Name
			if pName != "" && strings.ToLower(pName) == titleLower {
				isPerfTitle = true
				break
			}
		}

		if isPerfTitle {
			qWords := getWords(parsed.Name)
			tWords := getWords(scene.Title)

			extraCount := 0
			for _, qw := range qWords {
				if _, ok := stopWordsAndCodecs[qw]; ok {
					continue
				}
				matched := false
				for _, tw := range tWords {
					if qw == tw {
						matched = true
						break
					}
				}
				if !matched {
					extraCount++
				}
			}
			if extraCount >= 2 {
				score -= 150.0
			}
		}

		// Check for unmatched scene title words
		qWords3 := make(map[string]struct{})
		for _, w := range rxWord3.FindAllString(strings.ToLower(parsed.Name), -1) {
			qWords3[w] = struct{}{}
		}

		tWords3 := make(map[string]struct{})
		for _, w := range rxWord3.FindAllString(strings.ToLower(scene.Title), -1) {
			tWords3[w] = struct{}{}
		}

		pWords3 := make(map[string]struct{})
		for _, p := range scene.Performers {
			if p.Name != "" {
				for _, w := range rxWord3.FindAllString(strings.ToLower(p.Name), -1) {
					pWords3[w] = struct{}{}
				}
			}
		}

		sWords3 := make(map[string]struct{})
		for _, val := range []string{scene.Site, scene.Parent, scene.Network} {
			if val != "" {
				for _, w := range rxWord3.FindAllString(strings.ToLower(val), -1) {
					sWords3[w] = struct{}{}
				}
			}
		}

		allCandidateWords := make(map[string]struct{})
		for w := range tWords3 {
			allCandidateWords[w] = struct{}{}
		}
		for w := range pWords3 {
			allCandidateWords[w] = struct{}{}
		}
		for w := range sWords3 {
			allCandidateWords[w] = struct{}{}
		}
		for w := range stopWords {
			allCandidateWords[w] = struct{}{}
		}

		extraWords3Count := 0
		for qw := range qWords3 {
			if _, ok := stopWords[qw]; ok {
				continue
			}
			if len(qw) == 4 && rxBareYear.MatchString(qw) {
				continue
			}
			matched := false
			for cw := range allCandidateWords {
				if qw == cw || strings.Contains(qw, cw) || strings.Contains(cw, qw) {
					matched = true
					break
				}
			}
			if !matched {
				extraWords3Count++
			}
		}

		sharedTitleWordsCount := 0
		for qw := range qWords3 {
			if _, ok := stopWords[qw]; ok {
				continue
			}
			_, inPerf := pWords3[qw]
			_, inSite := sWords3[qw]
			if inPerf || inSite {
				continue
			}
			if _, inTitle := tWords3[qw]; inTitle {
				sharedTitleWordsCount++
			}
		}

		hasSharedTitle := sharedTitleWordsCount >= 2 || (sharedTitleWordsCount >= 1 && (perfMatch || siteMatched))

		if extraWords3Count >= 2 {
			if isPlatform {
				score -= 10.0
			} else if isOnlyPerformer && perfMatch && siteMatched {
				// skip
			} else {
				baseMult := 60.0
				baseCap := 150.0
				if hasSharedTitle || perfMatch {
					baseMult = 30.0
					baseCap = 75.0
				}
				wordPenalty := math.Min(baseCap, float64(extraWords3Count)*baseMult)
				score -= wordPenalty
			}
		}

		if !isOnlyPerformer {
			nameScore := fuzzy.Score(parsed.Name, scene.Title)
			score += nameScore

			qAll := getWords(parsed.Name)
			tAll := getWords(scene.Title)

			allowed2Letter := map[string]struct{}{"dp": {}, "bj": {}, "xx": {}, "vr": {}}

			qWordsFiltered := make(map[string]struct{})
			for _, w := range qAll {
				_, ok2 := allowed2Letter[w]
				_, okStop := stopWords[w]
				if (len(w) >= 3 || ok2) && !okStop {
					if !(len(w) == 4 && rxBareYear.MatchString(w)) {
						qWordsFiltered[w] = struct{}{}
					}
				}
			}

			tWordsFiltered := make(map[string]struct{})
			for _, w := range tAll {
				_, ok2 := allowed2Letter[w]
				_, okStop := stopWords[w]
				if (len(w) >= 3 || ok2) && !okStop {
					if !(len(w) == 4 && rxBareYear.MatchString(w)) {
						tWordsFiltered[w] = struct{}{}
					}
				}
			}

			sharedWords := make(map[string]struct{})
			for qw := range qWordsFiltered {
				for tw := range tWordsFiltered {
					if qw == tw || strings.Contains(qw, tw) || strings.Contains(tw, qw) {
						sharedWords[qw] = struct{}{}
						break
					}
				}
			}

			sharedNonGenericCount := 0
			for sw := range sharedWords {
				if _, ok := genericKeywords[sw]; !ok {
					sharedNonGenericCount++
				}
			}

			hasDurationMatch := false
			if targetDuration != nil && scene.Duration != nil {
				diff := math.Abs(*scene.Duration - *targetDuration)
				if diff <= 30.0 {
					hasDurationMatch = true
				}
			}

			hasLegitimateMatch := false
			if sharedNonGenericCount >= 1 {
				hasLegitimateMatch = true
			} else if len(sharedWords) >= 3 {
				hasLegitimateMatch = true
			} else if len(qWordsFiltered) > 0 && (float64(len(sharedWords))/float64(len(qWordsFiltered))) >= 0.6 {
				hasLegitimateMatch = true
			} else if hasDurationMatch {
				hasLegitimateMatch = true
			} else if perfMatch && siteMatched {
				// When both performer AND site match, this is already a very strong signal.
				// The -500 penalty would wrongly drop a correct match simply because the
				// scene title uses different wording than the torrent filename.
				hasLegitimateMatch = true
			} else if isOnlyPerformer && perfMatch {
				// Query name is purely performer-based; title word overlap is meaningless.
				hasLegitimateMatch = true
			}

			if !hasLegitimateMatch {
				score -= 500.0
			}
		}
	}

	// 7. Duration match scoring (Bonus only)
	if targetDuration != nil && scene.Duration != nil {
		diff := math.Abs(*scene.Duration - *targetDuration)
		if diff < 1.0 {
			score += 500.0
		} else if diff <= 3.0 {
			score += 300.0
		} else if diff <= 10.0 {
			score += 50.0
		}
	}

	return score
}

type candidateResult struct {
	Scene      scene.Scene
	Score      float64
	TieBreaker float64
}

func PickBest(parsed parse.ParsedFilename, candidates []scene.Scene, duration *float64) *scene.Scene {
	if len(candidates) == 0 {
		return nil
	}

	scored := make([]candidateResult, 0, len(candidates))
	for _, c := range candidates {
		s := ScoreResult(parsed, c, duration)

		tieBreaker := fuzzy.Score(parsed.Name, c.Title)
		if parsed.Name != "" && c.Title != "" {
			tieBreaker += float64(fuzzywuzzy.Ratio(strings.ToLower(parsed.Name), strings.ToLower(c.Title))) * 0.1
		}

		scored = append(scored, candidateResult{
			Scene:      c,
			Score:      s,
			TieBreaker: tieBreaker,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].TieBreaker > scored[j].TieBreaker
	})

	if len(scored) == 0 {
		return nil
	}

	best := &scored[0].Scene

	// Reorder performers: put the performer named in the filename first.
	// This gives consistent, intuitive ordering regardless of source DB ordering.
	// Pattern: Python output has filename-named performer first; Go was returning
	// source-DB order which varies (StashDB can be alphabetical or male-first).
	if parsed.Performer != "" && len(best.Performers) > 1 {
		parsedPerfClean := cleanAlphanumeric(unidecode.Unidecode(parsed.Performer))
		headlineIdx := -1
		for i, p := range best.Performers {
			pClean := cleanAlphanumeric(unidecode.Unidecode(p.Name))
			if strings.Contains(pClean, parsedPerfClean) || strings.Contains(parsedPerfClean, pClean) ||
				fuzzy.Score(parsed.Performer, p.Name) >= 85.0 {
				headlineIdx = i
				break
			}
		}
		if headlineIdx > 0 {
			// Bubble the headline performer to the front
			headline := best.Performers[headlineIdx]
			newPerfs := make([]scene.Performer, 0, len(best.Performers))
			newPerfs = append(newPerfs, headline)
			for i, p := range best.Performers {
				if i != headlineIdx {
					newPerfs = append(newPerfs, p)
				}
			}
			best.Performers = newPerfs
		}
	}

	return best
}
