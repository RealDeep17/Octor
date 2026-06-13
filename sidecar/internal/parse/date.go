package parse

import (
	"regexp"
	"strconv"
)

var (
	rxYYYYMMDD = regexp.MustCompile(`\b((?:19|20)\d{2})[.\-_ ]+(0[1-9]|1[0-2])[.\-_ ]+(0[1-9]|[12]\d|3[01])\b`)
	rxDDMMYYYY = regexp.MustCompile(`\b(0[1-9]|[12]\d|3[01])[.\-_ ]+(0[1-9]|1[0-2])[.\-_ ]+((?:19|20)\d{2})\b`)
	rxYYMMDD   = regexp.MustCompile(`\b(\d{2})[.\-_ ]+(0[1-9]|1[0-2])[.\-_ ]+(0[1-9]|[12]\d|3[01])\b`)
)

func ExtractDate(text string) (date string, start, end int, ok bool) {
	// 1. Try YYYY-MM-DD
	if idxs := rxYYYYMMDD.FindStringSubmatchIndex(text); idxs != nil {
		start, end = idxs[0], idxs[1]
		year := text[idxs[2]:idxs[3]]
		month := text[idxs[4]:idxs[5]]
		day := text[idxs[6]:idxs[7]]
		return year + "-" + month + "-" + day, start, end, true
	}

	// 2. Try DD-MM-YYYY
	if idxs := rxDDMMYYYY.FindStringSubmatchIndex(text); idxs != nil {
		start, end = idxs[0], idxs[1]
		day := text[idxs[2]:idxs[3]]
		month := text[idxs[4]:idxs[5]]
		year := text[idxs[6]:idxs[7]]
		return year + "-" + month + "-" + day, start, end, true
	}

	// 3. Try YY-MM-DD
	if idxs := rxYYMMDD.FindStringSubmatchIndex(text); idxs != nil {
		start, end = idxs[0], idxs[1]
		yyStr := text[idxs[2]:idxs[3]]
		month := text[idxs[4]:idxs[5]]
		day := text[idxs[6]:idxs[7]]
		yy, _ := strconv.Atoi(yyStr)
		var year string
		if yy < 50 {
			year = "20" + yyStr
		} else {
			year = "19" + yyStr
		}
		return year + "-" + month + "-" + day, start, end, true
	}

	return "", 0, 0, false
}
