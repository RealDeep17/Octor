package fuzzy

import (
	"math"
	"sort"
	"strings"

	fuzzywuzzy "github.com/paul-mannino/go-fuzzywuzzy"
)

func process(s string) string {
	s = strings.ToLower(s)
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

func ratio(s1, s2 string) int {
	return fuzzywuzzy.Ratio(s1, s2)
}

func partialRatio(s1, s2 string) int {
	return fuzzywuzzy.PartialRatio(s1, s2)
}

func processAndSort(s string) string {
	words := strings.Fields(s)
	sort.Strings(words)
	return strings.Join(words, " ")
}

func tokenSortRatio(s1, s2 string) int {
	sorted1 := processAndSort(s1)
	sorted2 := processAndSort(s2)
	return ratio(sorted1, sorted2)
}

func partialTokenSortRatio(s1, s2 string) int {
	sorted1 := processAndSort(s1)
	sorted2 := processAndSort(s2)
	return partialRatio(sorted1, sorted2)
}

func tokenSet(s1, s2 string, partial bool) int {
	if s1 == s2 {
		return 100
	}
	words1 := strings.Fields(s1)
	words2 := strings.Fields(s2)

	set1 := make(map[string]bool)
	for _, w := range words1 {
		set1[w] = true
	}
	set2 := make(map[string]bool)
	for _, w := range words2 {
		set2[w] = true
	}

	var intersection []string
	var diff1to2 []string
	for w := range set1 {
		if set2[w] {
			intersection = append(intersection, w)
		} else {
			diff1to2 = append(diff1to2, w)
		}
	}
	var diff2to1 []string
	for w := range set2 {
		if !set1[w] {
			diff2to1 = append(diff2to1, w)
		}
	}

	sort.Strings(intersection)
	sort.Strings(diff1to2)
	sort.Strings(diff2to1)

	sortedSect := strings.Join(intersection, " ")
	sorted1to2 := strings.Join(diff1to2, " ")
	sorted2to1 := strings.Join(diff2to1, " ")

	combined1to2 := sortedSect
	if sorted1to2 != "" {
		if combined1to2 != "" {
			combined1to2 += " " + sorted1to2
		} else {
			combined1to2 = sorted1to2
		}
	}

	combined2to1 := sortedSect
	if sorted2to1 != "" {
		if combined2to1 != "" {
			combined2to1 += " " + sorted2to1
		} else {
			combined2to1 = sorted2to1
		}
	}

	sortedSect = strings.TrimSpace(sortedSect)
	combined1to2 = strings.TrimSpace(combined1to2)
	combined2to1 = strings.TrimSpace(combined2to1)

	var ratioFunc func(string, string) int
	if partial {
		ratioFunc = partialRatio
	} else {
		ratioFunc = ratio
	}

	r1 := ratioFunc(sortedSect, combined1to2)
	r2 := ratioFunc(sortedSect, combined2to1)
	r3 := ratioFunc(combined1to2, combined2to1)

	maxVal := r1
	if r2 > maxVal {
		maxVal = r2
	}
	if r3 > maxVal {
		maxVal = r3
	}
	return maxVal
}

func tokenSetRatio(s1, s2 string) int {
	return tokenSet(s1, s2, false)
}

func partialTokenSetRatio(s1, s2 string) int {
	return tokenSet(s1, s2, true)
}

func wRatioProcessed(p1, p2 string) float64 {
	if p1 == "" || p2 == "" {
		return 0.0
	}

	tryPartial := true
	unbaseScale := 0.95
	partialScale := 0.90

	base := float64(ratio(p1, p2))
	len1 := float64(len(p1))
	len2 := float64(len(p2))
	var lenRatio float64
	if len1 > len2 {
		lenRatio = len1 / len2
	} else {
		lenRatio = len2 / len1
	}

	if lenRatio < 1.5 {
		tryPartial = false
	}
	if lenRatio > 8.0 {
		partialScale = 0.6
	}

	if tryPartial {
		partial := float64(partialRatio(p1, p2)) * partialScale
		ptsor := float64(partialTokenSortRatio(p1, p2)) * unbaseScale * partialScale
		ptser := float64(partialTokenSetRatio(p1, p2)) * unbaseScale * partialScale

		maxVal := base
		if partial > maxVal {
			maxVal = partial
		}
		if ptsor > maxVal {
			maxVal = ptsor
		}
		if ptser > maxVal {
			maxVal = ptser
		}
		return math.Round(maxVal)
	} else {
		tsor := float64(tokenSortRatio(p1, p2)) * unbaseScale
		tser := float64(tokenSetRatio(p1, p2)) * unbaseScale

		maxVal := base
		if tsor > maxVal {
			maxVal = tsor
		}
		if tser > maxVal {
			maxVal = tser
		}
		return math.Round(maxVal)
	}
}

func Score(query, candidate string) float64 {
	if query == "" || candidate == "" {
		return 0.0
	}
	q := process(query)
	c := process(candidate)
	if q == "" || c == "" {
		return 0.0
	}

	base := wRatioProcessed(q, c)

	// Short-string correction (mirrors Python)
	if len(candidate) < 15 {
		ratioVal := float64(ratio(q, c))
		partialVal := float64(partialRatio(q, c))
		if ratioVal < 40 && partialVal < 75 {
			base = math.Min(base, ratioVal*1.5)
		}
	}
	return base
}

func BestCandidateScore(query string, candidates []string) float64 {
	if query == "" || len(candidates) == 0 {
		return 0.0
	}
	best := 0.0
	for _, c := range candidates {
		s := Score(query, c)
		if s > best {
			best = s
		}
	}
	return best
}
