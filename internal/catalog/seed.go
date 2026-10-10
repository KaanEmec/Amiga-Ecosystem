package catalog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// seedImageExts are the file types copied from the seed images directory.
// SVG is deliberately excluded: it can carry script and is served same-origin.
var seedImageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".avif": true,
}

// SeedResult reports what Seed did. Seeded is false when the catalogue
// already had entries and nothing was touched.
type SeedResult struct {
	Seeded  bool
	Entries int
	Links   int // total related ids across entries
	Images  int // image files copied into the media dir
}

// Seed imports the seed catalogue and images, but only into an empty
// catalogue. Images are copied first (existing files are kept), so an
// interrupted seed can simply be re-run. Entries get rev 1 and a revision
// row with actor "seed".
func (c *Catalog) Seed(ctx context.Context, jsonPath, imagesDir, mediaDir string) (SeedResult, error) {
	n, err := c.store.Q().CountEntries(ctx)
	if err != nil {
		return SeedResult{}, err
	}
	if n > 0 {
		return SeedResult{}, nil
	}
	doc, err := LoadDocument(jsonPath)
	if err != nil {
		return SeedResult{}, fmt.Errorf("seed: %w", err)
	}
	entries, err := Validate(doc)
	if err != nil {
		return SeedResult{}, fmt.Errorf("seed %s: %w", jsonPath, err)
	}
	copied, err := CopyImages(imagesDir, mediaDir)
	if err != nil {
		return SeedResult{}, fmt.Errorf("seed images: %w", err)
	}
	if _, err := c.write(ctx, doc, "seed", "seed import", true); err != nil {
		if errors.Is(err, ErrNotEmpty) {
			// Another process seeded between the count and the write.
			return SeedResult{Images: copied}, nil
		}
		return SeedResult{}, fmt.Errorf("seed: %w", err)
	}
	res := SeedResult{Seeded: true, Entries: len(entries), Images: copied}
	for _, e := range entries {
		res.Links += len(e.Related)
	}
	return res, nil
}

// CopyImages copies image files from srcDir into mediaDir, preserving
// relative paths (images/essentials/x.jpg -> media/essentials/x.jpg).
// Hidden files, symlinks and non-image files are skipped, as are files
// already present in mediaDir. Writes go through os.Root so nothing can
// land outside mediaDir. It returns the number of files copied.
func CopyImages(srcDir, mediaDir string) (int, error) {
	src, err := os.OpenRoot(srcDir)
	if err != nil {
		return 0, fmt.Errorf("opening images dir: %w", err)
	}
	defer src.Close()
	if err := os.MkdirAll(mediaDir, 0o750); err != nil {
		return 0, fmt.Errorf("creating media dir: %w", err)
	}
	dst, err := os.OpenRoot(mediaDir)
	if err != nil {
		return 0, fmt.Errorf("opening media dir: %w", err)
	}
	defer dst.Close()

	copied := 0
	srcFS := src.FS()
	err = fs.WalkDir(srcFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !seedImageExts[strings.ToLower(path.Ext(p))] {
			return nil
		}
		ok, err := copyNew(srcFS, dst, p)
		if err != nil {
			return fmt.Errorf("copying %s: %w", p, err)
		}
		if ok {
			copied++
		}
		return nil
	})
	if err != nil {
		return copied, err
	}
	return copied, nil
}

// copyNew copies p from srcFS into dst unless it already exists there. The
// data is written to a temporary name and renamed into place so a partial
// copy is never mistaken for a complete file.
func copyNew(srcFS fs.FS, dst *os.Root, p string) (bool, error) {
	name := filepath.FromSlash(p)
	if _, err := dst.Stat(name); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if dir := filepath.Dir(name); dir != "." {
		if err := dst.MkdirAll(dir, 0o750); err != nil {
			return false, err
		}
	}
	in, err := srcFS.Open(p)
	if err != nil {
		return false, err
	}
	defer in.Close()

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return false, err
	}
	tmp := name + ".tmp-" + hex.EncodeToString(suffix[:])
	out, err := dst.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = dst.Remove(tmp)
		return false, err
	}
	if err := out.Close(); err != nil {
		_ = dst.Remove(tmp)
		return false, err
	}
	if err := dst.Rename(tmp, name); err != nil {
		_ = dst.Remove(tmp)
		return false, err
	}
	return true, nil
}
