package store

import (
	"context"
	"fmt"
)

// Image is one row of images: provenance of a published image file.
type Image struct {
	ID           int64
	SuggestionID *int64
	EntryID      *string
	SourceURL    string
	SourcePage   string
	Credit       string
	Path         string // relative to the media dir, e.g. suggested/a500-1a2b3c4d.jpg
	ContentType  string
	Width        int
	Height       int
	Bytes        int64
	SHA256       string
	CreatedAt    int64
}

// InsertImage records a published image. A row for the same path is kept
// as is (the file content is identical, as path embeds its hash); inserted
// reports whether a new row was written.
func (q Queries) InsertImage(ctx context.Context, im Image) (inserted bool, err error) {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO images (suggestion_id, entry_id, source_url, source_page, credit, path,
		                     content_type, width, height, bytes, sha256, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (path) DO NOTHING`,
		im.SuggestionID, im.EntryID, im.SourceURL, im.SourcePage, im.Credit, im.Path,
		im.ContentType, im.Width, im.Height, im.Bytes, im.SHA256, im.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("inserting image %s: %w", im.Path, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inserting image %s: %w", im.Path, err)
	}
	return n > 0, nil
}

// ListImagesByEntry returns the images published for an entry, newest first.
func (q Queries) ListImagesByEntry(ctx context.Context, entryID string) ([]Image, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, suggestion_id, entry_id, source_url, source_page, credit, path,
		        content_type, width, height, bytes, sha256, created_at
		 FROM images WHERE entry_id = ? ORDER BY created_at DESC, id DESC`, entryID)
	if err != nil {
		return nil, fmt.Errorf("listing images of %q: %w", entryID, err)
	}
	defer rows.Close()
	var out []Image
	for rows.Next() {
		var im Image
		if err := rows.Scan(&im.ID, &im.SuggestionID, &im.EntryID, &im.SourceURL, &im.SourcePage, &im.Credit, &im.Path,
			&im.ContentType, &im.Width, &im.Height, &im.Bytes, &im.SHA256, &im.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning image: %w", err)
		}
		out = append(out, im)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating images: %w", err)
	}
	return out, nil
}
