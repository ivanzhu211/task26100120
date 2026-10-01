package parking

import (
	"testing"
	"time"
)

func mustPlan(t *testing.T, zone string, bands []Band, discountBps int64) *Plan {
	t.Helper()
	p, err := CreatePlan(zone, bands, discountBps)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	return p
}

func mustQuote(t *testing.T, p *Plan, enter, exit string) *QuoteResult {
	t.Helper()
	en, err := time.Parse(time.RFC3339, enter)
	if err != nil {
		t.Fatalf("parse enter: %v", err)
	}
	ex, err := time.Parse(time.RFC3339, exit)
	if err != nil {
		t.Fatalf("parse exit: %v", err)
	}
	q, err := Quote(p, en, ex)
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	return q
}

func daySeconds(b DayBill) map[int]int64 {
	m := map[int]int64{}
	for _, bs := range b.Bands {
		m[bs.Band.StartMin] += bs.Seconds
	}
	return m
}

func totalSeconds(q *QuoteResult) int64 {
	var n int64
	for _, d := range q.Days {
		for _, bs := range d.Bands {
			n += bs.Seconds
		}
	}
	return n
}

func dateStr(t time.Time) string { return t.Format("2006-01-02") }

func TestFallBackNewYork(t *testing.T) {
	p := mustPlan(t, "America/New_York", []Band{
		{StartMin: 0, RatePerHour: 600},
		{StartMin: 90, RatePerHour: 1200},
		{StartMin: 120, RatePerHour: 600},
	}, 8000)
	q := mustQuote(t, p, "2024-11-03T04:00:00Z", "2024-11-03T08:00:00Z")

	if len(q.Days) != 1 || dateStr(q.Days[0].Date) != "2024-11-03" {
		t.Fatalf("days = %v", q.Days)
	}
	got := daySeconds(q.Days[0])
	want := map[int]int64{0: 7200, 90: 3600, 120: 3600}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("band %d: got %ds, want %ds", k, got[k], v)
		}
	}
	if q.Days[0].Amount != 2400 {
		t.Errorf("day amount = %d, want 2400", q.Days[0].Amount)
	}
	if q.Total != 2400 {
		t.Errorf("total = %d, want 2400", q.Total)
	}
}

func TestSpringForwardNewYork(t *testing.T) {
	p := mustPlan(t, "America/New_York", []Band{
		{StartMin: 0, RatePerHour: 600},
		{StartMin: 150, RatePerHour: 1200},
	}, 10000)
	q := mustQuote(t, p, "2024-03-10T06:30:00Z", "2024-03-10T07:30:00Z")

	if len(q.Days) != 1 {
		t.Fatalf("days = %v", q.Days)
	}
	got := daySeconds(q.Days[0])
	if got[0] != 1800 || got[150] != 1800 {
		t.Errorf("band seconds = %v, want 1800/1800", got)
	}
	if q.Days[0].Amount != 900 || q.Total != 900 {
		t.Errorf("amount = %d total = %d, want 900/900", q.Days[0].Amount, q.Total)
	}
}

func TestLordHoweHalfHourFallBack(t *testing.T) {
	p := mustPlan(t, "Australia/Lord_Howe", []Band{
		{StartMin: 0, RatePerHour: 600},
		{StartMin: 90, RatePerHour: 1200},
		{StartMin: 120, RatePerHour: 600},
	}, 10000)
	// 2024-04-07: 02:00 +11:00 falls back to 01:30 +10:30 (15:30Z).
	q := mustQuote(t, p, "2024-04-06T15:00:00Z", "2024-04-06T16:00:00Z")

	if len(q.Days) != 1 || dateStr(q.Days[0].Date) != "2024-04-07" {
		t.Fatalf("days = %v", q.Days)
	}
	got := daySeconds(q.Days[0])
	if got[120] != 1800 || got[90] != 1800 {
		t.Errorf("band seconds = %v, want {120:1800, 90:1800}", got)
	}
	if n := totalSeconds(q); n != 3600 {
		t.Errorf("total seconds = %d, want 3600", n)
	}
	if q.Total != 900 {
		t.Errorf("total = %d, want 900", q.Total)
	}
}

func TestApiaSkips20111230(t *testing.T) {
	p := mustPlan(t, "Pacific/Apia", []Band{
		{StartMin: 0, RatePerHour: 600},
	}, 10000)
	// 2011-12-30T10:00:00Z the offset jumps -10 -> +14, skipping 2011-12-30.
	q := mustQuote(t, p, "2011-12-30T09:00:00Z", "2011-12-30T12:00:00Z")

	if len(q.Days) != 2 {
		t.Fatalf("days = %v", q.Days)
	}
	if dateStr(q.Days[0].Date) != "2011-12-29" || dateStr(q.Days[1].Date) != "2011-12-31" {
		t.Errorf("dates = %s, %s; want 2011-12-29 and 2011-12-31 (no 12-30)",
			dateStr(q.Days[0].Date), dateStr(q.Days[1].Date))
	}
	if got := daySeconds(q.Days[0]); got[0] != 3600 {
		t.Errorf("12-29 seconds = %v, want 3600", got)
	}
	if got := daySeconds(q.Days[1]); got[0] != 7200 {
		t.Errorf("12-31 seconds = %v, want 7200", got)
	}
	if n := totalSeconds(q); n != 10800 {
		t.Errorf("total seconds = %d, want 10800", n)
	}
	if q.Days[0].Amount != 600 || q.Days[1].Amount != 1200 || q.Total != 1800 {
		t.Errorf("amounts = %d/%d total %d, want 600/1200/1800",
			q.Days[0].Amount, q.Days[1].Amount, q.Total)
	}
}

func TestCrossMidnightHalfCentRounding(t *testing.T) {
	p := mustPlan(t, "UTC", []Band{
		{StartMin: 0, RatePerHour: 1800},
	}, 10000)
	q := mustQuote(t, p, "2024-01-01T23:59:59Z", "2024-01-02T00:00:02Z")

	if len(q.Days) != 2 {
		t.Fatalf("days = %v", q.Days)
	}
	if got := daySeconds(q.Days[0]); got[0] != 1 {
		t.Errorf("day1 seconds = %v, want 1", got)
	}
	if got := daySeconds(q.Days[1]); got[0] != 2 {
		t.Errorf("day2 seconds = %v, want 2", got)
	}
	// day1: 1s * 1800c/h = 0.5c -> rounds up to 1; day2: 2s -> 1c.
	if q.Days[0].Amount != 1 || q.Days[1].Amount != 1 || q.Total != 2 {
		t.Errorf("amounts = %d/%d total %d, want 1/1/2",
			q.Days[0].Amount, q.Days[1].Amount, q.Total)
	}
}

func TestSplitSameRateBandKeepsAmount(t *testing.T) {
	enter, exit := "2024-06-15T08:00:00Z", "2024-06-15T20:00:00Z"
	single := mustPlan(t, "America/New_York", []Band{
		{StartMin: 0, RatePerHour: 700},
	}, 9000)
	split := mustPlan(t, "America/New_York", []Band{
		{StartMin: 0, RatePerHour: 700},
		{StartMin: 720, RatePerHour: 700},
	}, 9000)
	q1 := mustQuote(t, single, enter, exit)
	q2 := mustQuote(t, split, enter, exit)
	if q1.Total != q2.Total {
		t.Errorf("totals differ: single %d, split %d", q1.Total, q2.Total)
	}
	for i := range q1.Days {
		if q1.Days[i].Amount != q2.Days[i].Amount {
			t.Errorf("day %d amount differs: %d vs %d", i, q1.Days[i].Amount, q2.Days[i].Amount)
		}
	}
}

func TestDailyCap(t *testing.T) {
	p := mustPlan(t, "UTC", []Band{
		{StartMin: 0, RatePerHour: 1000},
	}, 10000)
	// 10 hours at 1000c/h = 10000c, capped at 2500 per day.
	q := mustQuote(t, p, "2024-01-01T00:00:00Z", "2024-01-02T10:00:00Z")
	if len(q.Days) != 2 {
		t.Fatalf("days = %v", q.Days)
	}
	if q.Days[0].Amount != 2500 || q.Days[1].Amount != 2500 {
		t.Errorf("amounts = %d/%d, want 2500/2500", q.Days[0].Amount, q.Days[1].Amount)
	}
	if q.Total != 5000 {
		t.Errorf("total = %d, want 5000 (cap does not accumulate across days)", q.Total)
	}
}

func TestLongStayFortyYearsFortyEightBands(t *testing.T) {
	bands := make([]Band, 48)
	for i := range bands {
		bands[i] = Band{StartMin: i * 30, RatePerHour: int64(100 + i*17)}
	}
	p := mustPlan(t, "America/New_York", bands, 8333)

	enter, _ := time.Parse(time.RFC3339, "2000-01-01T00:00:00Z")
	exit, _ := time.Parse(time.RFC3339, "2040-01-01T00:00:00Z")
	q, err := Quote(p, enter, exit)
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	want := exit.Unix() - enter.Unix()
	if n := totalSeconds(q); n != want {
		t.Errorf("total billed seconds = %d, want %d", n, want)
	}
	for i := 1; i < len(q.Days); i++ {
		if !q.Days[i-1].Date.Before(q.Days[i].Date) {
			t.Errorf("days not sorted at %d: %v !< %v", i, q.Days[i-1].Date, q.Days[i].Date)
		}
	}
	var total int64
	for _, d := range q.Days {
		if d.Amount < 0 || d.Amount > 2500 {
			t.Errorf("day %s amount %d out of [0,2500]", dateStr(d.Date), d.Amount)
		}
		total += d.Amount
	}
	if total != q.Total {
		t.Errorf("sum of day amounts %d != total %d", total, q.Total)
	}
}

func TestMidnightFallBackRepeatsDate(t *testing.T) {
	p := mustPlan(t, "America/Santiago", []Band{
		{StartMin: 0, RatePerHour: 600},
	}, 10000)
	// 2024-04-07T03:00:00Z: 00:00 -03 falls back to 2024-04-06 23:00 -04,
	// so the wall hour 23:00-24:00 of Apr 6 elapses twice.
	q := mustQuote(t, p, "2024-04-07T02:30:00Z", "2024-04-07T03:30:00Z")
	if len(q.Days) != 1 || dateStr(q.Days[0].Date) != "2024-04-06" {
		t.Fatalf("days = %v", q.Days)
	}
	if got := daySeconds(q.Days[0]); got[0] != 3600 {
		t.Errorf("seconds = %v, want 3600 on the repeated date", got)
	}
	if q.Total != 600 {
		t.Errorf("total = %d, want 600", q.Total)
	}
}

func TestCreatePlanValidation(t *testing.T) {
	if _, err := CreatePlan("No/Such_Zone", []Band{{StartMin: 0}}, 10000); err == nil {
		t.Error("bad zone accepted")
	}
	if _, err := CreatePlan("UTC", nil, 10000); err == nil {
		t.Error("empty bands accepted")
	}
	if _, err := CreatePlan("UTC", []Band{{StartMin: 5}}, 10000); err == nil {
		t.Error("first band not at 0 accepted")
	}
	if _, err := CreatePlan("UTC", []Band{{StartMin: 0}, {StartMin: 0}}, 10000); err == nil {
		t.Error("non-increasing bands accepted")
	}
	if _, err := CreatePlan("UTC", []Band{{StartMin: 0, RatePerHour: -1}}, 10000); err == nil {
		t.Error("negative rate accepted")
	}
	if _, err := CreatePlan("UTC", []Band{{StartMin: 0}}, 10001); err == nil {
		t.Error("discountBps > 10000 accepted")
	}
	many := make([]Band, 49)
	for i := range many {
		many[i].StartMin = i
	}
	if _, err := CreatePlan("UTC", many, 10000); err == nil {
		t.Error("49 bands accepted")
	}
}
