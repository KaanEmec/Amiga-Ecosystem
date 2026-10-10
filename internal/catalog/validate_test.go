package catalog

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	seed := loadSeed(t)
	if _, err := Validate(seed); err != nil {
		t.Fatalf("seed does not validate: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"unknown category", func(m map[string]any) { m["category"] = "toasters" }, `unknown category "toasters"`},
		{"unknown kind", func(m map[string]any) { m["kind"] = "toaster" }, `unknown kind "toaster"`},
		{"unknown status", func(m map[string]any) { m["status"] = "lost" }, `unknown status "lost"`},
		{"unknown arch", func(m map[string]any) { m["arch"] = []any{"z80"} }, `unknown arch "z80"`},
		{"empty arch", func(m map[string]any) { m["arch"] = []any{} }, "arch needs at least one"},
		{"dangling related", func(m map[string]any) { m["related"] = []any{"does-not-exist"} }, `related "does-not-exist" does not exist`},
		{"bad image", func(m map[string]any) { m["image"] = "/etc/passwd" }, `image "/etc/passwd"`},
		{"image path traversal", func(m map[string]any) { m["image"] = "images/../amiga.db" }, "image "},
		{"javascript url", func(m map[string]any) { m["url"] = "javascript:alert(1)" }, `url "javascript:alert(1)" must be an absolute http(s) URL`},
		{"relative url", func(m map[string]any) { m["url"] = "//evil.example/x" }, "must be an absolute http(s) URL"},
		{"weight out of range", func(m map[string]any) { m["weight"] = 6 }, "weight 6 must be 1-5"},
		{"missing weight", func(m map[string]any) { delete(m, "weight") }, "weight is required"},
		{"missing what", func(m map[string]any) { delete(m, "what") }, "what is required"},
		{"missing year", func(m map[string]any) { delete(m, "year") }, "year is required"},
		{"year as string", func(m map[string]any) { m["year"] = "1985" }, "decoding entry"},
		{"bad id", func(m map[string]any) { m["id"] = "Bad_ID" }, "must be a lowercase slug"},
		{"duplicate id", func(m map[string]any) { m["id"] = "a500" }, `duplicate id "a500"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := mutateEntry(t, seed, 0, tt.mutate)
			_, err := Validate(doc)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Validate() err = %v, want *ValidationError", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidate_Sections(t *testing.T) {
	t.Parallel()
	seed := loadSeed(t)

	noTours := *seed
	noTours.Tours = nil
	if _, err := Validate(&noTours); err == nil || !strings.Contains(err.Error(), `missing "tours"`) {
		t.Errorf("missing tours: err = %v", err)
	}

	badTour := *seed
	badTour.Tours = []byte(`[{"id":"t","steps":[{"id":"ghost"}]}]`)
	if _, err := Validate(&badTour); err == nil || !strings.Contains(err.Error(), `unknown entry "ghost"`) {
		t.Errorf("unknown tour step: err = %v", err)
	}
}

func TestValidImageRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"images/essentials/a1000.jpg", true},
		{"./local.png", true},
		{"https://example.com/x.png", true},
		{"http://example.com/x.png", true},
		{"data:image/png;base64,AAAA", true},
		{"/abs/path.png", false},
		{"javascript:alert(1)", false},
		{"images/../../secret", false},
		{"ftp://example.com/x.png", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := validImageRef(tt.in); got != tt.want {
				t.Errorf("validImageRef(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
