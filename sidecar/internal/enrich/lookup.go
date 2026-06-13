package enrich

import (
	"context"
	"regexp"
	"strings"

	"github.com/webtor-io/sidecar/internal/detect"
	"github.com/webtor-io/sidecar/internal/fuzzy"
	"github.com/webtor-io/sidecar/internal/parse"
	"github.com/webtor-io/sidecar/internal/scene"
	"github.com/webtor-io/sidecar/internal/score"
	"github.com/webtor-io/sidecar/internal/stashdb"
	"github.com/webtor-io/sidecar/internal/tpdb"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var genericWords = map[string]struct{}{
	"hardcore":        {},
	"sex":             {},
	"exposed":         {},
	"titties":         {},
	"stepmom":         {},
	"stepsister":      {},
	"caring":          {},
	"sharing":         {},
	"sharingiscaring": {},
	"huge":            {},
	"tits":            {},
	"fucking":         {},
	"anal":            {},
	"pussy":           {},
	"first":           {},
	"teacher":         {},
	"stepparent":      {},
	"stepdad":         {},
	"stepson":         {},
}

func getSearchTerms(name string) []string {
	if name == "" {
		return nil
	}
	terms := []string{name}
	words := strings.Fields(name)
	if len(words) > 2 {
		firstTwo := strings.Join(words[:2], " ")
		if _, isGeneric := genericWords[strings.ToLower(firstTwo)]; !isGeneric {
			terms = append(terms, firstTwo)
		}

		lastTwo := strings.Join(words[len(words)-2:], " ")
		allLastTwoGeneric := true
		for _, w := range words[len(words)-2:] {
			if _, isGeneric := genericWords[strings.ToLower(w)]; !isGeneric {
				allLastTwoGeneric = false
				break
			}
		}
		if !allLastTwoGeneric {
			found := false
			for _, t := range terms {
				if t == lastTwo {
					found = true
					break
				}
			}
			if !found {
				terms = append(terms, lastTwo)
			}
		}
	}

	var result []string
	for _, t := range terms {
		if len(t) > 2 {
			result = append(result, t)
		}
	}
	return result
}

func AdultEnrichmentLookup(
	ctx context.Context,
	title string,
	duration *float64,
	tpdbCl *tpdb.Client,
	stashdbCl *stashdb.Client,
) (*scene.Scene, float64) {
	parsed := parse.ParseAdultFilename(title)

	// ── JAV fast-path ──────────────────────────────────────────────────────────
	if code, ok := detect.ExtractJAVCode(title); ok {
		log.Infof("Detected JAV code: %s", code)
		if results, err := tpdbCl.JAVSearch(code, 5); err == nil && len(results) > 0 {
			best := score.PickBest(parsed, results, duration)
			if best != nil {
				// Re-score the chosen JAV best match to check if it meets the threshold
				s := score.ScoreResult(parsed, *best, duration)
				return best, s
			}
		}
		log.Warnf("JAV lookup missed for %s, falling through to scene search", code)
	}

	site := parsed.Site
	date := parsed.Date
	name := parsed.Name

	type searchTask func() ([]scene.Scene, error)
	var tasks []searchTask

	// ── Task Set 1: Structural Passes ──────────────────────────────────────────
	if site != "" || date != "" || name != "" {
		tasks = append(tasks, func() ([]scene.Scene, error) {
			return tpdbCl.Search(site, date, name, 10)
		})
	}
	if date != "" {
		tasks = append(tasks, func() ([]scene.Scene, error) {
			return tpdbCl.Search(site, "", name, 10)
		})
	}
	if name != "" {
		tasks = append(tasks, func() ([]scene.Scene, error) {
			return tpdbCl.Search(site, date, "", 10)
		})
	}
	if site != "" {
		tasks = append(tasks, func() ([]scene.Scene, error) {
			return tpdbCl.Search("", date, name, 10)
		})
	}
	nameOrTitle := name
	if nameOrTitle == "" {
		nameOrTitle = title
	}
	tasks = append(tasks, func() ([]scene.Scene, error) {
		return tpdbCl.Search("", "", nameOrTitle, 10)
	})

	// ── Task Set 2: Fallbacks & StashDB ────────────────────────────────────────
	clean := parse.NameCleaner(title)
	nameOrClean := name
	if nameOrClean == "" {
		nameOrClean = clean
	}
	searchTerms := getSearchTerms(nameOrClean)

	// Always append the search terms of the full cleaned title to prevent query term loss due to eager parsing
	if name != "" {
		for _, ct := range getSearchTerms(clean) {
			found := false
			for _, t := range searchTerms {
				if t == ct {
					found = true
					break
				}
			}
			if !found {
				searchTerms = append(searchTerms, ct)
			}
		}
	}

	performer := parsed.Performer
	if performer != "" {
		// Insert performer at top of search terms if not present
		found := false
		for _, t := range searchTerms {
			if t == performer {
				found = true
				break
			}
		}
		if !found {
			searchTerms = append([]string{performer}, searchTerms...)
		}

		if site != "" {
			sitePerf := site + " " + performer
			foundSP := false
			for _, t := range searchTerms {
				if t == sitePerf {
					foundSP = true
					break
				}
			}
			if !foundSP {
				searchTerms = append([]string{sitePerf}, searchTerms...)
			}
		}
	}

	for _, term := range searchTerms {
		termVal := term
		if site != "" {
			tasks = append(tasks, func() ([]scene.Scene, error) {
				return tpdbCl.Search(site, "", termVal, 25)
			})
		}
		tasks = append(tasks, func() ([]scene.Scene, error) {
			return tpdbCl.SearchRaw(termVal, 25)
		})
		tasks = append(tasks, func() ([]scene.Scene, error) {
			return stashdbCl.Search(termVal)
		})
	}

	log.Infof("Executing %d search tasks in parallel...", len(tasks))

	taskResults := make([][]scene.Scene, len(tasks))
	sem := make(chan struct{}, 32)
	g, _ := errgroup.WithContext(ctx)

	for i, t := range tasks {
		idx := i
		taskVal := t
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := taskVal()
			if err == nil {
				taskResults[idx] = res
			}
			return nil
		})
	}
	_ = g.Wait()

	var allCandidates []scene.Scene
	seenIds := make(map[string]bool)
	for _, resList := range taskResults {
		for _, r := range resList {
			rid := r.ID
			if r.Source == "stashdb" {
				rid = "stashdb:" + rid
			}
			if rid != "" && !seenIds[rid] {
				seenIds[rid] = true
				allCandidates = append(allCandidates, r)
			}
		}
	}

	best := score.PickBest(parsed, allCandidates, duration)
	if best == nil {
		log.Infof("No candidates found for '%s'", title)
		return nil, 0.0
	}

	mergeScenes(best, allCandidates)

	s := score.ScoreResult(parsed, *best, duration)
	return best, s
}

var rxAlphanumeric = regexp.MustCompile(`[^a-z0-9]`)

func cleanAlphanumeric(s string) string {
	return rxAlphanumeric.ReplaceAllString(strings.ToLower(s), "")
}

// isBetterDescription returns true if the candidate description c should replace the current b.
// Rule: only fill a missing/empty description. Never overwrite an existing one from a merge —
// the best-scored candidate's description is the authoritative one.
func isBetterDescription(b, c string) bool {
	if b == "" || b == "N/A" {
		return c != "" && c != "N/A"
	}
	return false
}

// isBetterPoster returns true if candidate poster c is better than current best b.
// Priority: theporndb.net CDN > any non-stashdb > stashdb.
func isBetterPoster(b, c string) bool {
	if c == "" || c == "N/A" {
		return false
	}
	if b == "" || b == "N/A" {
		return true
	}
	bHasTPDB := strings.Contains(b, "theporndb.net")
	cHasTPDB := strings.Contains(c, "theporndb.net")
	// Never replace a theporndb.net URL with anything else
	if bHasTPDB && !cHasTPDB {
		return false
	}
	// Prefer theporndb.net over stashdb
	if !bHasTPDB && cHasTPDB {
		return true
	}
	return false
}

// mergeScenes enriches best with supplementary data from other candidates that
// are provably the SAME scene (exact URL match OR exact source+ID match).
// We do NOT merge across different scenes — that causes actor/tag/genre bloat.
func mergeScenes(best *scene.Scene, candidates []scene.Scene) {
	perfMap := make(map[string]bool)
	for _, p := range best.Performers {
		perfMap[strings.ToLower(strings.TrimSpace(p.Name))] = true
	}

	tagMap := make(map[string]bool)
	for _, t := range best.Tags {
		tagMap[strings.ToLower(strings.TrimSpace(t))] = true
	}

	for _, c := range candidates {
		// Skip self
		if c.Source == best.Source && c.ID == best.ID {
			continue
		}

		// Only merge when we are 100% sure it's the same scene:
		// exact URL match OR fuzzy title match above 95% on same site
		same := false
		if best.URL != "" && c.URL != "" && best.URL == c.URL {
			same = true
		}
		// Strong title match on same source (e.g. stashdb alias for same TPDB scene)
		if !same && best.Source == c.Source && best.ID != "" && best.ID == c.ID {
			same = true
		}
		// Cross-source: same scene URL resolves to both TPDB and StashDB records
		if !same && best.URL != "" && c.URL != "" {
			// Already handled above
		}
		// Fuzzy title match at 97%+ with same site as very strong signal
		if !same && best.Site != "" && c.Site != "" {
			bestSiteClean := cleanAlphanumeric(best.Site)
			cSiteClean := cleanAlphanumeric(c.Site)
			if bestSiteClean == cSiteClean && best.Title != "" && c.Title != "" {
				if fuzzy.Score(best.Title, c.Title) >= 97.0 {
					same = true
				}
			}
		}

		if same {
			// Merge performers: alias-aware dedup to avoid bloat from cross-source naming differences.
			// e.g. TPDB "Lety" + StashDB "Lety Howl" = same person (substring → alias → skip).
			for _, p := range c.Performers {
				pName := strings.TrimSpace(p.Name)
				if pName == "" {
					continue
				}
				key := strings.ToLower(pName)

				if perfMap[key] {
					continue // exact duplicate
				}

				// Alias check: new name is substring of existing (or vice versa) → same person
				isAlias := false
				for existing := range perfMap {
					if strings.Contains(existing, key) || strings.Contains(key, existing) {
						isAlias = true
						break
					}
					// Fuzzy fallback: catches typos like "Porto De Bilbao" / "Potro De Bilbao"
					if fuzzy.Score(pName, existing) >= 82.0 {
						isAlias = true
						break
					}
				}
				if !isAlias {
					perfMap[key] = true
					best.Performers = append(best.Performers, p)
				}
			}

			// Merge tags (only from same source, to avoid cross-site tag pollution)
			if c.Source == best.Source {
				for _, t := range c.Tags {
					tName := strings.TrimSpace(t)
					if tName == "" {
						continue
					}
					key := strings.ToLower(tName)
					if !tagMap[key] {
						tagMap[key] = true
						best.Tags = append(best.Tags, t)
					}
				}
			}

			// Merge description: only fill if missing
			if isBetterDescription(best.Description, c.Description) {
				best.Description = c.Description
			}

			// Merge poster: prefer theporndb.net CDN over stashdb
			if isBetterPoster(best.Poster, c.Poster) {
				best.Poster = c.Poster
			}

			// Merge URL
			if (best.URL == "" || best.URL == "N/A") && c.URL != "" && c.URL != "N/A" {
				best.URL = c.URL
			}

			// Merge duration
			if best.Duration == nil && c.Duration != nil {
				best.Duration = c.Duration
			}

			// Merge rating
			if best.Rating == nil && c.Rating != nil {
				best.Rating = c.Rating
			}
		}
	}
}

