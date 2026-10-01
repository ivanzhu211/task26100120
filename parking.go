// Package parking implements per-stay parking billing with local-time
// rate bands, member discount and per-local-day caps.
package parking

import (
	"errors"
	"fmt"
	"time"
)

// MaxBands is the maximum number of rate bands per plan.
const MaxBands = 48

// Band is a single rate band within a local day.
type Band struct {
	// StartMin is the local start minute of the band, 0..1439.
	StartMin int
	// CentsPerHour is the non-negative rate in integer cents per hour.
	CentsPerHour int64
}

// Plan is a billing plan created by CreatePlan.
type Plan struct {
	loc         *time.Location
	bands       []Band
	discountBps int64
}

// DayBill is the bill for one local calendar day.
type DayBill struct {
	// Date is the local date in "YYYY-MM-DD" form.
	Date string
	// Seconds[i] is the number of elapsed seconds billed at bands[i].
	Seconds []int64
	// Amount is the rounded, capped amount for the day in integer cents.
	Amount int64
}

// Quote is the result of a billing computation.
type Quote struct {
	Days  []DayBill
	Total int64
}

// CreatePlan builds a billing plan. zone is an IANA time zone name.
// bands are sorted by strictly increasing StartMin, the first band starts
// at minute 0 and the last band runs to end of day. discountBps is in
// [0, 10000]; 8000 means charging 80% of the list price.
func CreatePlan(zone string, bands []Band, discountBps int) (*Plan, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("parking: load zone %q: %w", zone, err)
	}
	if len(bands) == 0 {
		return nil, errors.New("parking: at least one band is required")
	}
	if len(bands) > MaxBands {
		return nil, fmt.Errorf("parking: at most %d bands", MaxBands)
	}
	if bands[0].StartMin != 0 {
		return nil, errors.New("parking: first band must start at minute 0")
	}
	for i, b := range bands {
		if b.StartMin < 0 || b.StartMin >= 24*60 {
			return nil, fmt.Errorf("parking: band %d start minute %d out of range", i, b.StartMin)
		}
		if i > 0 && b.StartMin <= bands[i-1].StartMin {
			return nil, fmt.Errorf("parking: band %d start minute not strictly increasing", i)
		}
		if b.CentsPerHour < 0 {
			return nil, fmt.Errorf("parking: band %d rate must be non-negative", i)
		}
	}
	if discountBps < 0 || discountBps > 10000 {
		return nil, fmt.Errorf("parking: discountBps %d out of range [0, 10000]", discountBps)
	}
	cp := make([]Band, len(bands))
	copy(cp, bands)
	return &Plan{loc: loc, bands: cp, discountBps: int64(discountBps)}, nil
}
