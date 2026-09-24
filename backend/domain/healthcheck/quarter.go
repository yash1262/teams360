package healthcheck

import (
	"fmt"
	"regexp"
	"strconv"
)

var (
	quarterPatternQuarterly  = regexp.MustCompile(`^(\d{4}) Q([1-4])$`)
	quarterPatternHalfYearly = regexp.MustCompile(`^(\d{4}) H([12])$`)
	quarterPatternMonthly    = regexp.MustCompile(`^(\d{4}) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)$`)
	quarterPatternYearly     = regexp.MustCompile(`^(\d{4})$`)
	quarterPatternLegacyHalf = regexp.MustCompile(`^(\d{4}) - (1st|2nd) Half$`)
	quarterMonthAbbrToNumber = map[string]int{
		"Jan": 1, "Feb": 2, "Mar": 3, "Apr": 4, "May": 5, "Jun": 6,
		"Jul": 7, "Aug": 8, "Sep": 9, "Oct": 10, "Nov": 11, "Dec": 12,
	}
)

// PeriodQuarter derives the calendar quarter (1-4) and year that an assessment period
// falls into, regardless of the team's cadence. Non-quarterly cadences are mapped to the
// quarter containing the period's start month, so the same-quarter duplicate-submission
// rule (see NextEligibleQuarter) applies uniformly across cadences:
//   - Quarterly "YYYY Qn"           -> quarter n, year YYYY
//   - Half-yearly "YYYY H1"/"YYYY H2" -> Q1/Q3, year YYYY
//   - Monthly "YYYY Mon"            -> the quarter containing that month, year YYYY
//   - Yearly "YYYY"                 -> Q1, year YYYY
//   - Legacy "YYYY - 1st/2nd Half"  -> Q3 YYYY / Q1 YYYY+1
//
// ok is false when period does not match any known assessment-period format.
func PeriodQuarter(period string) (quarter int, year int, ok bool) {
	if m := quarterPatternQuarterly.FindStringSubmatch(period); m != nil {
		y, _ := strconv.Atoi(m[1])
		q, _ := strconv.Atoi(m[2])
		return q, y, true
	}
	if m := quarterPatternHalfYearly.FindStringSubmatch(period); m != nil {
		y, _ := strconv.Atoi(m[1])
		if m[2] == "1" {
			return 1, y, true
		}
		return 3, y, true
	}
	if m := quarterPatternMonthly.FindStringSubmatch(period); m != nil {
		y, _ := strconv.Atoi(m[1])
		month := quarterMonthAbbrToNumber[m[2]]
		return (month-1)/3 + 1, y, true
	}
	if m := quarterPatternYearly.FindStringSubmatch(period); m != nil {
		y, _ := strconv.Atoi(m[1])
		return 1, y, true
	}
	if m := quarterPatternLegacyHalf.FindStringSubmatch(period); m != nil {

		y, _ := strconv.Atoi(m[1])
		if m[2] == "1st" {
			return 3, y, true
		}
		return 1, y + 1, true
	}
	return 0, 0, false
}

// NextEligibleQuarter returns the next quarter (and year) that is eligible for a new
// submission: two quarters after the given quarter/year, rolling the year over as needed.
//
//	Q1 -> Q3 (same year)
//	Q2 -> Q4 (same year)
//	Q3 -> Q1 (next year)
//	Q4 -> Q2 (next year)
func NextEligibleQuarter(quarter, year int) (nextQuarter int, nextYear int) {
	total := (quarter - 1) + 2
	nextQuarter = total%4 + 1
	nextYear = year + total/4
	return nextQuarter, nextYear
}

// FormatQuarterPeriod renders a quarter/year pair for user-facing messages, e.g. "Q1 2026".
func FormatQuarterPeriod(quarter, year int) string {
	return fmt.Sprintf("Q%d %d", quarter, year)
}

// IsConsecutiveQuarter reports whether toQuarter/toYear is exactly one calendar quarter away
// from fromQuarter/fromYear, in either direction (e.g. Q1 2026 -> Q2 2026, or Q4 2025 ->
// Q1 2026). Used to enforce the "no consecutive quarter" submission restriction, which
// requires at least one full quarter to be skipped between submissions.
func IsConsecutiveQuarter(fromQuarter, fromYear, toQuarter, toYear int) bool {
	from := fromYear*4 + (fromQuarter - 1)
	to := toYear*4 + (toQuarter - 1)
	diff := to - from
	if diff < 0 {
		diff = -diff
	}
	return diff == 1
}
