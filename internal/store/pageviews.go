package store

import (
	"context"
	"fmt"
	"time"
)

// sessionGap is the idle time after which a visitor's next page view starts
// a new session.
const sessionGap = "1800" // 30 minutes, in seconds

// Pageview is one recorded public page view.
type Pageview struct {
	At       int64
	Day      string // UTC yyyy-mm-dd of At
	Path     string
	Visitor  string // daily-rotating hash
	Referrer string // host only, "" for direct or same-site
}

// PageStats aggregates page views over a period.
type PageStats struct {
	Pageviews int
	Visitors  int
	Sessions  int
}

// DayStats is PageStats for one UTC day.
type DayStats struct {
	Day string
	PageStats
}

// TopRow is one row of a top-N breakdown (path or referrer host).
type TopRow struct {
	Key       string
	Pageviews int
	Visitors  int
}

// InsertPageview records one page view.
func (q Queries) InsertPageview(ctx context.Context, p Pageview) error {
	if _, err := q.db.ExecContext(ctx,
		"INSERT INTO pageviews (at, day, path, visitor, referrer) VALUES (?, ?, ?, ?, ?)",
		p.At, p.Day, p.Path, p.Visitor, p.Referrer); err != nil {
		return fmt.Errorf("inserting pageview: %w", err)
	}
	return nil
}

// PrunePageviews deletes page views older than before.
func (q Queries) PrunePageviews(ctx context.Context, before int64) error {
	if _, err := q.db.ExecContext(ctx, "DELETE FROM pageviews WHERE at < ?", before); err != nil {
		return fmt.Errorf("pruning pageviews: %w", err)
	}
	return nil
}

// sessionsCTE yields the page views since ? with gap = seconds since the
// same visitor's previous view (NULL for the first). A view starts a session
// when gap is NULL or above sessionGap; visitor ids rotate daily, so
// sessions never span a UTC midnight.
const sessionsCTE = `WITH v AS (
	SELECT day, visitor, at - LAG(at) OVER (PARTITION BY visitor ORDER BY at, id) AS gap
	FROM pageviews WHERE at >= ?)`

// startsSession counts the session-starting rows of v.
const startsSession = "SUM(gap IS NULL OR gap > " + sessionGap + ")"

// StatsSummary returns page views, unique visitors and sessions since sinceAt.
func (q Queries) StatsSummary(ctx context.Context, sinceAt int64) (PageStats, error) {
	var s PageStats
	err := q.db.QueryRowContext(ctx, sessionsCTE+`
		SELECT COUNT(*), COUNT(DISTINCT visitor), COALESCE(`+startsSession+`, 0) FROM v`, sinceAt).
		Scan(&s.Pageviews, &s.Visitors, &s.Sessions)
	if err != nil {
		return PageStats{}, fmt.Errorf("summarising pageviews: %w", err)
	}
	return s, nil
}

// DailyStats returns one row per UTC day for the days days ending with
// today's date, newest first; days without views are zero.
func (q Queries) DailyStats(ctx context.Context, today time.Time, days int) ([]DayStats, error) {
	if days <= 0 {
		return nil, nil
	}
	today = today.UTC()
	first := time.Date(today.Year(), today.Month(), today.Day()-(days-1), 0, 0, 0, 0, time.UTC)
	rows, err := q.db.QueryContext(ctx, sessionsCTE+`
		SELECT day, COUNT(*), COUNT(DISTINCT visitor), `+startsSession+` FROM v GROUP BY day`, first.Unix())
	if err != nil {
		return nil, fmt.Errorf("daily pageviews: %w", err)
	}
	defer rows.Close()
	byDay := map[string]PageStats{}
	for rows.Next() {
		var day string
		var s PageStats
		if err := rows.Scan(&day, &s.Pageviews, &s.Visitors, &s.Sessions); err != nil {
			return nil, fmt.Errorf("scanning daily pageviews: %w", err)
		}
		byDay[day] = s
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating daily pageviews: %w", err)
	}
	out := make([]DayStats, days)
	for i := range out {
		day := first.AddDate(0, 0, days-1-i).Format(time.DateOnly)
		out[i] = DayStats{Day: day, PageStats: byDay[day]}
	}
	return out, nil
}

// TopPaths returns the most viewed paths since sinceAt.
func (q Queries) TopPaths(ctx context.Context, sinceAt int64, limit int) ([]TopRow, error) {
	return q.top(ctx, "path", "", sinceAt, limit)
}

// TopReferrers returns the referring hosts with the most views since sinceAt.
func (q Queries) TopReferrers(ctx context.Context, sinceAt int64, limit int) ([]TopRow, error) {
	return q.top(ctx, "referrer", "AND referrer != ''", sinceAt, limit)
}

// top groups page views by col (a fixed column name, never user input).
func (q Queries) top(ctx context.Context, col, filter string, sinceAt int64, limit int) ([]TopRow, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT `+col+`, COUNT(*) AS n, COUNT(DISTINCT visitor)
		FROM pageviews WHERE at >= ? `+filter+`
		GROUP BY `+col+` ORDER BY n DESC, `+col+` LIMIT ?`, sinceAt, limit)
	if err != nil {
		return nil, fmt.Errorf("top %s: %w", col, err)
	}
	defer rows.Close()
	var out []TopRow
	for rows.Next() {
		var r TopRow
		if err := rows.Scan(&r.Key, &r.Pageviews, &r.Visitors); err != nil {
			return nil, fmt.Errorf("scanning top %s: %w", col, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating top %s: %w", col, err)
	}
	return out, nil
}
