package parking

import (
	"testing"
	"time"
)

func mustPlan(t *testing.T, zone string, bands []Band, discountBps int) *Plan {
	t.Helper()
	p, err := CreatePlan(zone, bands, discountBps)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	return p
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts
}

func TestNewYorkFallBack(t *testing.T) {
	p := mustPlan(t, "America/New_York", []Band{
		{StartMin: 0, CentsPerHour: 600},
		{StartMin: 90, CentsPerHour: 1200},
		{StartMin: 120, CentsPerHour: 600},
	}, 8000)
	q := p.Quote(mustTime(t, "2024-11-03T04:00:00Z"), mustTime(t, "2024-11-03T08:00:00Z"))
	if len(q.Days) != 1 || q.Days[0].Date != "2024-11-03" {
		t.Fatalf("days = %+v", q.Days)
	}
	want := []int64{7200, 3600, 3600}
	for i, w := range want {
		if q.Days[0].Seconds[i] != w {
			t.Fatalf("band %d seconds = %d, want %d (all %v)", i, q.Days[0].Seconds[i], w, q.Days[0].Seconds)
		}
	}
	if q.Days[0].Amount != 2400 {
		t.Fatalf("amount = %d, want 2400", q.Days[0].Amount)
	}
	if q.Total != 2400 {
		t.Fatalf("total = %d, want 2400", q.Total)
	}
}

func TestNewYorkSpringForward(t *testing.T) {
	p := mustPlan(t, "America/New_York", []Band{
		{StartMin: 0, CentsPerHour: 600},
		{StartMin: 150, CentsPerHour: 1200},
	}, 10000)
	q := p.Quote(mustTime(t, "2024-03-10T06:30:00Z"), mustTime(t, "2024-03-10T07:30:00Z"))
	if len(q.Days) != 1 || q.Days[0].Date != "2024-03-10" {
		t.Fatalf("days = %+v", q.Days)
	}
	want := []int64{1800, 1800}
	for i, w := range want {
		if q.Days[0].Seconds[i] != w {
			t.Fatalf("band %d seconds = %d, want %d (all %v)", i, q.Days[0].Seconds[i], w, q.Days[0].Seconds)
		}
	}
	if q.Days[0].Amount != 900 {
		t.Fatalf("amount = %d, want 900", q.Days[0].Amount)
	}
}

func TestLordHoweHalfHourFallBack(t *testing.T) {
	// 2024-04-07: clocks fall back 02:00 +11 -> 01:30 +10:30,
	// repeating the 01:30-02:00 wall interval after only 30 minutes.
	p := mustPlan(t, "Australia/Lord_Howe", []Band{
		{StartMin: 0, CentsPerHour: 600},
		{StartMin: 90, CentsPerHour: 1200},
	}, 10000)
	q := p.Quote(mustTime(t, "2024-04-06T14:00:00Z"), mustTime(t, "2024-04-06T16:00:00Z"))
	if len(q.Days) != 1 || q.Days[0].Date != "2024-04-07" {
		t.Fatalf("days = %+v", q.Days)
	}
	want := []int64{1800, 5400}
	for i, w := range want {
		if q.Days[0].Seconds[i] != w {
			t.Fatalf("band %d seconds = %d, want %d (all %v)", i, q.Days[0].Seconds[i], w, q.Days[0].Seconds)
		}
	}
	var sum int64
	for _, s := range q.Days[0].Seconds {
		sum += s
	}
	if sum != 7200 {
		t.Fatalf("total seconds = %d, want 7200", sum)
	}
	// (1800*600 + 5400*1200) / 3600 = 2100
	if q.Days[0].Amount != 2100 {
		t.Fatalf("amount = %d, want 2100", q.Days[0].Amount)
	}
}

func TestApiaSkippedDay(t *testing.T) {
	// Pacific/Apia skipped 2011-12-30 entirely (offset -10 -> +14).
	p := mustPlan(t, "Pacific/Apia", []Band{
		{StartMin: 0, CentsPerHour: 600},
	}, 10000)
	enter := mustTime(t, "2011-12-29T08:00:00Z")
	exit := mustTime(t, "2011-12-31T08:00:00Z")
	q := p.Quote(enter, exit)
	wantDates := []string{"2011-12-28", "2011-12-29", "2011-12-31"}
	if len(q.Days) != len(wantDates) {
		t.Fatalf("days = %+v", q.Days)
	}
	wantSeconds := []int64{7200, 86400, 79200}
	var totalSeconds int64
	for i, d := range q.Days {
		if d.Date != wantDates[i] {
			t.Fatalf("day %d date = %s, want %s", i, d.Date, wantDates[i])
		}
		if d.Seconds[0] != wantSeconds[i] {
			t.Fatalf("day %s seconds = %d, want %d", d.Date, d.Seconds[0], wantSeconds[i])
		}
		totalSeconds += d.Seconds[0]
	}
	if totalSeconds != exit.Unix()-enter.Unix() {
		t.Fatalf("total seconds = %d, want %d", totalSeconds, exit.Unix()-enter.Unix())
	}
	// 1200 + min(14400, 2500) + min(13200, 2500) = 6200
	if q.Total != 6200 {
		t.Fatalf("total = %d, want 6200", q.Total)
	}
}

func TestCrossMidnightHalfCentRounding(t *testing.T) {
	p := mustPlan(t, "UTC", []Band{
		{StartMin: 0, CentsPerHour: 1},
	}, 10000)
	q := p.Quote(mustTime(t, "2024-06-01T23:00:00Z"), mustTime(t, "2024-06-02T00:30:00Z"))
	if len(q.Days) != 2 {
		t.Fatalf("days = %+v", q.Days)
	}
	if q.Days[0].Date != "2024-06-01" || q.Days[0].Seconds[0] != 3600 {
		t.Fatalf("day0 = %+v", q.Days[0])
	}
	if q.Days[1].Date != "2024-06-02" || q.Days[1].Seconds[0] != 1800 {
		t.Fatalf("day1 = %+v", q.Days[1])
	}
	// Day 1: 3600s * 1c/h = 1 cent. Day 2: 1800s = exactly 0.5 cent,
	// which rounds up to 1.
	if q.Days[0].Amount != 1 || q.Days[1].Amount != 1 {
		t.Fatalf("amounts = %d, %d; want 1, 1", q.Days[0].Amount, q.Days[1].Amount)
	}
	if q.Total != 2 {
		t.Fatalf("total = %d, want 2", q.Total)
	}
}

func TestSplitSamePriceBandKeepsAmount(t *testing.T) {
	bands := []Band{
		{StartMin: 0, CentsPerHour: 600},
		{StartMin: 90, CentsPerHour: 1200},
		{StartMin: 120, CentsPerHour: 600},
	}
	split := []Band{
		{StartMin: 0, CentsPerHour: 600},
		{StartMin: 60, CentsPerHour: 600},
		{StartMin: 90, CentsPerHour: 1200},
		{StartMin: 120, CentsPerHour: 600},
		{StartMin: 200, CentsPerHour: 600},
	}
	enter := mustTime(t, "2024-11-03T04:00:00Z")
	exit := mustTime(t, "2024-11-03T08:00:00Z")
	a := mustPlan(t, "America/New_York", bands, 8000).Quote(enter, exit)
	b := mustPlan(t, "America/New_York", split, 8000).Quote(enter, exit)
	if len(a.Days) != len(b.Days) {
		t.Fatalf("day counts differ: %d vs %d", len(a.Days), len(b.Days))
	}
	for i := range a.Days {
		if a.Days[i].Date != b.Days[i].Date || a.Days[i].Amount != b.Days[i].Amount {
			t.Fatalf("day %d differs: %+v vs %+v", i, a.Days[i], b.Days[i])
		}
	}
	if a.Total != b.Total {
		t.Fatalf("totals differ: %d vs %d", a.Total, b.Total)
	}
}

func TestFortyYearsFortyEightBands(t *testing.T) {
	bands := make([]Band, 48)
	for i := range bands {
		bands[i] = Band{StartMin: i * 30, CentsPerHour: int64((i%5 + 1) * 100)}
	}
	p := mustPlan(t, "America/New_York", bands, 9000)
	enter := mustTime(t, "2000-01-01T00:00:00Z")
	exit := mustTime(t, "2040-01-01T00:00:00Z")
	q := p.Quote(enter, exit)

	var totalSeconds, amountSum int64
	prev := ""
	for _, d := range q.Days {
		if d.Date <= prev {
			t.Fatalf("days not strictly increasing: %q after %q", d.Date, prev)
		}
		prev = d.Date
		if len(d.Seconds) != 48 {
			t.Fatalf("day %s has %d bands", d.Date, len(d.Seconds))
		}
		for _, s := range d.Seconds {
			totalSeconds += s
		}
		amountSum += d.Amount
	}
	if want := exit.Unix() - enter.Unix(); totalSeconds != want {
		t.Fatalf("total seconds = %d, want %d", totalSeconds, want)
	}
	if amountSum != q.Total {
		t.Fatalf("total = %d, sum of days = %d", q.Total, amountSum)
	}
	// 40 years including leap days must produce roughly 14610 day bills.
	if len(q.Days) < 14600 || len(q.Days) > 14620 {
		t.Fatalf("got %d day bills", len(q.Days))
	}
}

func TestCreatePlanValidation(t *testing.T) {
	good := []Band{{StartMin: 0, CentsPerHour: 600}}
	if _, err := CreatePlan("No/Such_Zone", good, 10000); err == nil {
		t.Fatal("expected error for bad zone")
	}
	if _, err := CreatePlan("UTC", nil, 10000); err == nil {
		t.Fatal("expected error for empty bands")
	}
	if _, err := CreatePlan("UTC", []Band{{StartMin: 5, CentsPerHour: 1}}, 10000); err == nil {
		t.Fatal("expected error for first band not at 0")
	}
	if _, err := CreatePlan("UTC", []Band{{StartMin: 0, CentsPerHour: 1}, {StartMin: 0, CentsPerHour: 2}}, 10000); err == nil {
		t.Fatal("expected error for non-increasing starts")
	}
	if _, err := CreatePlan("UTC", good, 10001); err == nil {
		t.Fatal("expected error for bad discount")
	}
	if _, err := CreatePlan("UTC", good, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
