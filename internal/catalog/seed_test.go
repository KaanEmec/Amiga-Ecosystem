package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalog_Seed(t *testing.T) {
	t.Parallel()
	cat, st := newTestCatalog(t)
	ctx := t.Context()
	media := filepath.Join(t.TempDir(), "media")

	res, err := cat.Seed(ctx, seedJSON, seedImages, media)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Seeded || res.Entries != 333 || res.Links != 422 || res.Images != 159 {
		t.Errorf("Seed() = %+v, want 333 entries, 422 links, 159 images", res)
	}
	if n, _ := st.Q().CountEntries(ctx); n != 333 {
		t.Errorf("entries in db = %d, want 333", n)
	}
	revs, err := st.Q().ListRevisions(ctx, "lorraine")
	if err != nil || len(revs) != 1 || revs[0].Actor != "seed" || revs[0].Rev != 1 {
		t.Errorf("seed revision = %+v, %v", revs, err)
	}
	if _, err := os.Stat(filepath.Join(media, "essentials", "a1000.jpg")); err != nil {
		t.Errorf("images/essentials/a1000.jpg not copied to media/essentials: %v", err)
	}
	if _, err := os.Stat(filepath.Join(media, "essentials", "SOURCES.md")); err == nil {
		t.Error("non-image file copied into media")
	}

	again, err := cat.Seed(ctx, seedJSON, seedImages, media)
	if err != nil {
		t.Fatal(err)
	}
	if again != (SeedResult{}) {
		t.Errorf("second Seed() = %+v, want no-op", again)
	}
	if n, _ := st.Q().CountRevisions(ctx); n != 333 {
		t.Errorf("revisions after second seed = %d, want 333", n)
	}
}

func TestCopyImages(t *testing.T) {
	t.Parallel()
	src, dst := t.TempDir(), t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a/one.png", "new")
	write("a/two.JPG", "two")
	write("a/notes.md", "skip")
	write(".hidden/x.png", "skip")
	write("a/evil.svg", "skip")
	if err := os.Symlink("/etc/hosts", filepath.Join(src, "a", "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "a", "one.png"), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := CopyImages(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("copied %d files, want 1", n)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a", "one.png")); string(b) != "kept" {
		t.Errorf("existing file overwritten: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a", "two.JPG")); string(b) != "two" {
		t.Errorf("two.JPG = %q", b)
	}
	for _, skipped := range []string{"a/notes.md", ".hidden/x.png", "a/evil.svg", "a/link.png"} {
		if _, err := os.Lstat(filepath.Join(dst, skipped)); err == nil {
			t.Errorf("%s should not be copied", skipped)
		}
	}
}
