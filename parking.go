// Package parking computes time-of-day rated parking fees with member
// discounts and per-local-day caps.
package parking

import (
	"fmt"
	"sort"
	"time"
)

// Band is a single time-of-day rate band. StartMin is the local
// minute-of-day (0..1439) at which the band begins; the band lasts until
// the next band's StartMin, and the last band lasts until end of day.
// RatePerHour is a non-negative integer amount in cents per hour.
type Band struct {
	StartMin    int
	RatePerHour int64
}

// Plan is a billing plan created by CreatePlan.
type Plan struct {
	loc         *time.Location
	bands       []Band
	discountBps int64
}

// BandSeconds records how many seconds of a parking stay fell into one
// rate band of one local day.
type BandSeconds struct {
	Band    Band
	Seconds int64
}

// DayBill is the bill for one local calendar day.
type DayBill struct {
	Date   time.Time // midnight of the local day, in the plan's zone
	Bands  []BandSeconds
	Amount int64 // cents, after discount, cap and rounding
}

// QuoteResult is the result of Quote: per-day bills and the grand total.
type QuoteResult struct {
	Days  []DayBill
	Total int64
}

// CreatePlan builds a billing plan for the given IANA zone, rate bands
// and member discount in basis points (8000 = charge 80% of list price).
func CreatePlan(zone string, bands []Band, discountBps int64) (*Plan, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("parking: load zone %q: %w", zone, err)
	}
	if len(bands) == 0 || len(bands) > 48 {
		return nil, fmt.Errorf("parking: need 1..48 bands, got %d", len(bands))
	}
	if bands[0].StartMin != 0 {
		return nil, fmt.Errorf("parking: first band must start at minute 0, got %d", bands[0].StartMin)
	}
	for i, b := range bands {
		if b.StartMin < 0 || b.StartMin >= 1440 {
			return nil, fmt.Errorf("parking: band %d start minute %d out of range", i, b.StartMin)
		}
		if i > 0 && b.StartMin <= bands[i-1].StartMin {
			return nil, fmt.Errorf("parking: band %d start minute %d not strictly increasing", i, b.StartMin)
		}
		if b.RatePerHour < 0 {
			return nil, fmt.Errorf("parking: band %d has negative rate", i)
		}
	}
	if discountBps < 0 || discountBps > 10000 {
		return nil, fmt.Errorf("parking: discountBps %d out of range 0..10000", discountBps)
	}
	cp := make([]Band, len(bands))
	copy(cp, bands)
	return &Plan{loc: loc, bands: cp, discountBps: discountBps}, nil
}

// Quote bills the half-open interval [enterUTC, exitUTC) second by
// second against the plan.
func Quote(p *Plan, enterUTC, exitUTC time.Time) (*QuoteResult, error) {
	if p == nil || p.loc == nil {
		return nil, fmt.Errorf("parking: nil plan")
	}
	enter := enterUTC.UTC().Truncate(time.Second)
	exit := exitUTC.UTC().Truncate(time.Second)
	if !enter.Before(exit) {
		return nil, fmt.Errorf("parking: enter %v not before exit %v", enterUTC, exitUTC)
	}

	offsetAt := func(unixSec int64) int {
		_, off := time.Unix(unixSec, 0).In(p.loc).Zone()
		return off
	}
	// firstChange returns the first unix second in (lo, hi] whose UTC
	// offset differs from off. Requires offset(lo)==off, offset(hi)!=off.
	firstChange := func(lo, hi int64, off int) int64 {
		for lo+1 < hi {
			mid := (lo + hi) / 2
			if offsetAt(mid) != off {
				hi = mid
			} else {
				lo = mid
			}
		}
		return hi
	}

	type dayKey struct{ y, m, d int }
	acc := map[dayKey][]int64{}
	addSecs := func(k dayKey, bandIdx int, secs int64) {
		per, ok := acc[k]
		if !ok {
			per = make([]int64, len(p.bands))
			acc[k] = per
		}
		per[bandIdx] += secs
	}

	// Walk the interval event by event. Events are rate-band boundaries
	// (including local midnight) and UTC offset transitions; between two
	// events both the band and the offset are constant.
	now := enter.Unix()
	end := exit.Unix()
	off := offsetAt(now)
	for now < end {
		wall := time.Unix(now, 0).In(p.loc)
		y, mo, d := wall.Date()
		h, mi, s := wall.Clock()
		ws := h*3600 + mi*60 + s

		bi := 0
		for i, b := range p.bands {
			if b.StartMin*60 <= ws {
				bi = i
			} else {
				break
			}
		}
		nextWs := 86400
		if bi+1 < len(p.bands) {
			nextWs = p.bands[bi+1].StartMin * 60
		}

		cand := now + int64(nextWs-ws)
		if cand > end {
			cand = end
		}
		// Detect a UTC offset transition inside (now, cand]. Real zones
		// never change offset twice within a day, but check the midpoint
		// too as cheap insurance.
		if mid := (now + cand) / 2; mid > now && offsetAt(mid) != off {
			cand = firstChange(now, mid, off)
		} else if offsetAt(cand) != off {
			cand = firstChange(now, cand, off)
		}

		if secs := cand - now; secs > 0 {
			addSecs(dayKey{y, int(mo), d}, bi, secs)
		}
		now = cand
		off = offsetAt(now)
	}

	keys := make([]dayKey, 0, len(acc))
	for k := range acc {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].y != keys[j].y {
			return keys[i].y < keys[j].y
		}
		if keys[i].m != keys[j].m {
			return keys[i].m < keys[j].m
		}
		return keys[i].d < keys[j].d
	})

	const denom = int64(3600 * 10000) // seconds/hour * basis points
	res := &QuoteResult{}
	for _, k := range keys {
		per := acc[k]
		bill := DayBill{
			Date: time.Date(k.y, time.Month(k.m), k.d, 0, 0, 0, 0, p.loc),
		}
		var num int64 // sum(seconds*rate) * discountBps, over denom cents
		for i, secs := range per {
			if secs == 0 {
				continue
			}
			bill.Bands = append(bill.Bands, BandSeconds{Band: p.bands[i], Seconds: secs})
			num += secs * p.bands[i].RatePerHour * p.discountBps
		}
		// Cap at 2500 cents, then round half up to whole cents.
		if num > 2500*denom {
			bill.Amount = 2500
		} else {
			bill.Amount = (num + denom/2) / denom
		}
		res.Days = append(res.Days, bill)
		res.Total += bill.Amount
	}
	return res, nil
}
