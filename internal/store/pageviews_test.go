package store

import (
	"slices"
	"testing"
	"time"
)

func TestQueries_PageviewStats(t *testing.T) {
	t.Parallel()
	st := openTest(t)
	ctx := t.Context()
	q := st.Q()
	day := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	at := day.Add(9 * time.Hour).Unix()
	views := []Pageview{
		{At: at, Path: "/", Visitor: "a", Referrer: "example.org"},
		{At: at + 10*60, Path: "/suggestions", Visitor: "a"},
		{At: at + 2*3600, Path: "/", Visitor: "a"},
		// Two days earlier: outside the summary window, inside the daily one.
		{At: at - 2*86400, Path: "/", Visitor: "b", Referrer: "example.org"},
	}
	for _, p := range views {
		p.Day = time.Unix(p.At, 0).UTC().Format(time.DateOnly)
		if err := q.InsertPageview(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	got, err := q.StatsSummary(ctx, day.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if want := (PageStats{Pageviews: 3, Visitors: 1, Sessions: 2}); got != want {
		t.Errorf("StatsSummary = %+v, want %+v", got, want)
	}

	daily, err := q.DailyStats(ctx, day.Add(23*time.Hour), 3)
	if err != nil {
		t.Fatal(err)
	}
	wantDaily := []DayStats{
		{Day: "2026-10-10", PageStats: PageStats{3, 1, 2}},
		{Day: "2026-10-09"},
		{Day: "2026-10-08", PageStats: PageStats{1, 1, 1}},
	}
	if !slices.Equal(daily, wantDaily) {
		t.Errorf("DailyStats = %+v, want %+v", daily, wantDaily)
	}

	paths, err := q.TopPaths(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []TopRow{{"/", 3, 2}, {"/suggestions", 1, 1}}; !slices.Equal(paths, want) {
		t.Errorf("TopPaths = %+v, want %+v", paths, want)
	}
	refs, err := q.TopReferrers(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []TopRow{{"example.org", 2, 2}}; !slices.Equal(refs, want) {
		t.Errorf("TopReferrers = %+v, want %+v", refs, want)
	}

	if err := q.PrunePageviews(ctx, day.Unix()); err != nil {
		t.Fatal(err)
	}
	if all, err := q.StatsSummary(ctx, 0); err != nil || all.Pageviews != 3 {
		t.Errorf("after prune: %+v, %v", all, err)
	}
}
