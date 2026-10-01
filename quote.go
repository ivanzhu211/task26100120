package parking

import (
	"sort"
	"time"
)

// dayCapCents is the per-local-day cap in cents.
const dayCapCents int64 = 2500

// offsetAt returns the zone offset in seconds at the given UTC unix time.
func offsetAt(loc *time.Location, unix int64) int64 {
	_, off := time.Unix(unix, 0).In(loc).Zone()
	return int64(off)
}

// wallToUTC maps a local wall time (date + minute of day) to the UTC unix
// instants at which it occurs: 0 instants if the wall time is skipped by a
// forward jump, 2 if it is repeated by a backward jump, else 1.
func wallToUTC(loc *time.Location, year int, month time.Month, day, minOfDay int) []int64 {
	guess := time.Date(year, month, day, minOfDay/60, minOfDay%60, 0, 0, time.UTC).Unix()
	seen := make(map[int64]bool)
	var out []int64
	// Offset changes never exceed 24h, so probing offsets within +/-48h of
	// the wall time interpreted as UTC finds every real occurrence.
	for _, delta := range []int64{-48 * 3600, -24 * 3600, 0, 24 * 3600, 48 * 3600} {
		cand := guess - offsetAt(loc, guess+delta)
		if seen[cand] {
			continue
		}
		t := time.Unix(cand, 0).In(loc)
		if t.Year() == year && t.Month() == month && t.Day() == day &&
			t.Hour() == minOfDay/60 && t.Minute() == minOfDay%60 && t.Second() == 0 {
			seen[cand] = true
			out = append(out, cand)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// findTransitions returns the UTC instants in (a, b] at which the zone
// offset changes, given that the offsets at a and b differ.
func findTransitions(loc *time.Location, a, b int64) []int64 {
	offA := offsetAt(loc, a)
	offB := offsetAt(loc, b)
	if offA == offB {
		return nil
	}
	// Binary search for the first instant in (a, b] whose offset != offA.
	lo, hi := a, b
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if offsetAt(loc, mid) == offA {
			lo = mid
		} else {
			hi = mid
		}
	}
	rest := findTransitions(loc, hi, b)
	return append([]int64{hi}, rest...)
}

// bandIndex returns the index of the band covering the given minute of day.
func (p *Plan) bandIndex(minOfDay int) int {
	idx := sort.Search(len(p.bands), func(i int) bool {
		return p.bands[i].StartMin > minOfDay
	})
	return idx - 1
}

// Quote computes the bill for the half-open interval [enterUTC, exitUTC).
func (p *Plan) Quote(enterUTC, exitUTC time.Time) *Quote {
	loc := p.loc
	enter := enterUTC.Unix()
	exit := exitUTC.Unix()

	// Collect candidate event instants: every UTC instant at which a local
	// band boundary (including midnight) occurs, for every local date that
	// can overlap the stay.
	eventSet := map[int64]struct{}{enter: {}, exit: {}}
	first := time.Unix(enter, 0).In(loc).AddDate(0, 0, -2)
	last := time.Unix(exit, 0).In(loc).AddDate(0, 0, 2)
	for date := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.UTC); !date.After(last); date = date.AddDate(0, 0, 1) {
		for _, b := range p.bands {
			for _, u := range wallToUTC(loc, date.Year(), date.Month(), date.Day(), b.StartMin) {
				if u > enter && u < exit {
					eventSet[u] = struct{}{}
				}
			}
		}
	}
	events := sortedKeys(eventSet)

	// Offset transitions inside the stay are events too: a forward jump can
	// change the applicable band without any band boundary occurring.
	for i := 0; i+1 < len(events); i++ {
		for _, t := range findTransitions(loc, events[i], events[i+1]) {
			if t > enter && t < exit {
				eventSet[t] = struct{}{}
			}
		}
	}
	events = sortedKeys(eventSet)

	// Attribute every elapsed second to its local date and rate band.
	type dayKey struct{ date string }
	seconds := make(map[dayKey][]int64)
	var order []dayKey
	for i := 0; i+1 < len(events); i++ {
		a, b := events[i], events[i+1]
		if a < enter {
			a = enter
		}
		if b > exit {
			b = exit
		}
		if a >= b {
			continue
		}
		mid := time.Unix(a+(b-a)/2, 0).In(loc)
		key := dayKey{mid.Format("2006-01-02")}
		band := p.bandIndex(mid.Hour()*60 + mid.Minute())
		sec, ok := seconds[key]
		if !ok {
			sec = make([]int64, len(p.bands))
			seconds[key] = sec
			order = append(order, key)
		}
		sec[band] += b - a
	}

	q := &Quote{}
	for _, key := range order {
		sec := seconds[key]
		amount := p.dayAmount(sec)
		q.Days = append(q.Days, DayBill{Date: key.date, Seconds: sec, Amount: amount})
		q.Total += amount
	}
	return q
}

// dayAmount converts per-band seconds into the capped, discounted, rounded
// day amount in integer cents, using exact integer arithmetic throughout.
func (p *Plan) dayAmount(seconds []int64) int64 {
	var num int64 // cents*seconds/hour, scaled by discountBps
	for i, s := range seconds {
		num += s * p.bands[i].CentsPerHour
	}
	num *= p.discountBps
	const den = int64(3600 * 10000)
	if cap := dayCapCents * den; num > cap {
		num = cap
	}
	return (num + den/2) / den
}

func sortedKeys(set map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
