package healthcheck

import "testing"

func TestNextEligibleQuarter(t *testing.T) {
	cases := []struct {
		name        string
		quarter     int
		year        int
		wantQuarter int
		wantYear    int
	}{
		{"Q1 rolls to Q3 same year", 1, 2026, 3, 2026},
		{"Q2 rolls to Q4 same year", 2, 2026, 4, 2026},
		{"Q3 rolls to Q1 next year", 3, 2026, 1, 2027},
		{"Q4 rolls to Q2 next year", 4, 2026, 2, 2027},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQuarter, gotYear := NextEligibleQuarter(tc.quarter, tc.year)
			if gotQuarter != tc.wantQuarter || gotYear != tc.wantYear {
				t.Errorf("NextEligibleQuarter(%d, %d) = (%d, %d), want (%d, %d)",
					tc.quarter, tc.year, gotQuarter, gotYear, tc.wantQuarter, tc.wantYear)
			}
		})
	}
}

func TestPeriodQuarter(t *testing.T) {
	cases := []struct {
		name        string
		period      string
		wantQuarter int
		wantYear    int
		wantOK      bool
	}{
		{"quarterly Q1", "2026 Q1", 1, 2026, true},
		{"quarterly Q4", "2026 Q4", 4, 2026, true},
		{"half-yearly H1 maps to Q1", "2026 H1", 1, 2026, true},
		{"half-yearly H2 maps to Q3", "2026 H2", 3, 2026, true},
		{"monthly Feb maps to Q1", "2026 Feb", 1, 2026, true},
		{"monthly Aug maps to Q3", "2026 Aug", 3, 2026, true},
		{"yearly maps to Q1", "2026", 1, 2026, true},
		{"legacy 1st half maps to Q3 same year", "2024 - 1st Half", 3, 2024, true},
		{"legacy 2nd half maps to Q1 next year", "2024 - 2nd Half", 1, 2025, true},
		{"invalid format", "not-a-period", 0, 0, false},
		{"empty string", "", 0, 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQuarter, gotYear, gotOK := PeriodQuarter(tc.period)
			if gotOK != tc.wantOK {
				t.Fatalf("PeriodQuarter(%q) ok = %v, want %v", tc.period, gotOK, tc.wantOK)
			}
			if !gotOK {
				return
			}
			if gotQuarter != tc.wantQuarter || gotYear != tc.wantYear {
				t.Errorf("PeriodQuarter(%q) = (%d, %d), want (%d, %d)",
					tc.period, gotQuarter, gotYear, tc.wantQuarter, tc.wantYear)
			}
		})
	}
}

func TestFormatQuarterPeriod(t *testing.T) {
	if got := FormatQuarterPeriod(1, 2026); got != "Q1 2026" {
		t.Errorf("FormatQuarterPeriod(1, 2026) = %q, want %q", got, "Q1 2026")
	}
}

func TestIsConsecutiveQuarter(t *testing.T) {
	cases := []struct {
		name                  string
		fromQuarter, fromYear int
		toQuarter, toYear     int
		want                  bool
	}{
		{"Q1 to Q2 same year is consecutive", 1, 2026, 2, 2026, true},
		{"Q2 to Q3 same year is consecutive", 2, 2026, 3, 2026, true},
		{"Q3 to Q4 same year is consecutive", 3, 2026, 4, 2026, true},
		{"Q4 to Q1 next year is consecutive", 4, 2025, 1, 2026, true},
		{"Q1 to Q3 same year skips a quarter", 1, 2026, 3, 2026, false},
		{"Q2 to Q4 same year skips a quarter", 2, 2026, 4, 2026, false},
		{"Q1 to Q4 same year skips two quarters", 1, 2026, 4, 2026, false},
		{"Q3 to Q1 next year skips a quarter", 3, 2025, 1, 2026, false},
		{"Q4 to Q2 next year skips a quarter", 4, 2025, 2, 2026, false},
		{"same quarter is not consecutive", 1, 2026, 1, 2026, false},
		{"reverse direction Q2 to Q1 is consecutive", 2, 2026, 1, 2026, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsConsecutiveQuarter(tc.fromQuarter, tc.fromYear, tc.toQuarter, tc.toYear); got != tc.want {
				t.Errorf("IsConsecutiveQuarter(%d, %d, %d, %d) = %v, want %v",
					tc.fromQuarter, tc.fromYear, tc.toQuarter, tc.toYear, got, tc.want)
			}
		})
	}
}
