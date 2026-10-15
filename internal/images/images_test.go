package images

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
)

func newTestFetcher(t *testing.T, allowLoopback bool) (*Fetcher, *store.Store, config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := st.Q().InsertEntry(t.Context(), store.Entry{ID: "a500", Doc: []byte(`{"id":"a500"}`), Rev: 1, Ord: 0}); err != nil {
		t.Fatal(err)
	}
	f := New(cfg, st, cfg.MediaDir())
	if allowLoopback {
		f.vet = func(a netip.Addr) error {
			if a.IsLoopback() {
				return nil
			}
			return vetAddr(a)
		}
	}
	return f, st, cfg
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngHeader is a PNG signature plus a valid IHDR claiming w x h pixels.
func pngHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2 // 8-bit RGB
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestVetAddr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		addr    string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"100.100.100.200", true},
		{"0.0.0.0", true},
		{"224.0.0.1", true},
		{"255.255.255.255", true},
		{"::1", true},
		{"::", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"fd00:ec2::254", true},
		{"ff02::1", true},
		{"::ffff:10.0.0.1", true},
		{"64:ff9b::a00:1", true},
		{"2002:a00:1::1", true},
		{"93.184.216.34", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()
			err := vetAddr(netip.MustParseAddr(tt.addr))
			if got := err != nil; got != tt.blocked {
				t.Errorf("vetAddr(%s) = %v, blocked want %v", tt.addr, err, tt.blocked)
			}
		})
	}
}

func TestParseURL(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "ftp://x.org/a.png", "file:///etc/passwd", "javascript:alert(1)",
		"https://user:pw@example.org/a.png", "https:///a.png", "//example.org/a.png", "https://" + strings.Repeat("a", 3000)} {
		if _, err := ParseURL(raw); !errors.Is(err, ErrURL) {
			t.Errorf("ParseURL(%.40q) = %v, want ErrURL", raw, err)
		}
	}
	if _, err := ParseURL("https://example.org/a.png"); err != nil {
		t.Error(err)
	}
}

func TestFetcher_Stage_Publish(t *testing.T) {
	t.Parallel()
	f, st, cfg := newTestFetcher(t, true)
	body := pngBytes(t, 4, 3)
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/img", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream") // sniffed, not trusted
		_, _ = w.Write(body)
	})
	ctx := t.Context()

	staged, err := f.Stage(ctx, srv.URL+"/redirect")
	if err != nil {
		t.Fatal(err)
	}
	if staged.ContentType != "image/png" || staged.Width != 4 || staged.Height != 3 || staged.Bytes != int64(len(body)) ||
		!strings.HasSuffix(staged.FinalURL, "/img") {
		t.Errorf("staged = %+v", staged)
	}
	fi, err := os.Stat(filepath.Join(cfg.StagingDir(), staged.Name))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("staged mode = %v, want 0640", fi.Mode().Perm())
	}

	t.Run("tampered staged name", func(t *testing.T) {
		bad := staged
		bad.Name = "../amiga.db"
		if _, err := f.Publish(bad, "a500", "", "", nil); !errors.Is(err, ErrStaged) {
			t.Errorf("err = %v, want ErrStaged", err)
		}
		bad = staged
		bad.SHA256 = strings.Repeat("0", 64)
		if _, err := f.Publish(bad, "a500", "", "", nil); !errors.Is(err, ErrStaged) {
			t.Errorf("hash mismatch: err = %v, want ErrStaged", err)
		}
	})

	pub, err := f.Publish(staged, "a500", "Photo: Someone, CC BY 4.0", "https://example.org/page", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ims, err := st.Q().ListImagesByEntry(ctx, "a500"); err != nil || len(ims) != 0 {
		t.Errorf("images rows before Record = %+v, %v", ims, err)
	}
	if err := st.Tx(ctx, func(tx *sql.Tx) error { return pub.Record(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
	path := pub.Path
	want := "images/suggested/a500-" + staged.SHA256[:8] + ".png"
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(filepath.Join(cfg.MediaDir(), strings.TrimPrefix(path, "images/")))
	if err != nil || !bytes.Equal(got, body) {
		t.Errorf("published file mismatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.StagingDir(), staged.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Error("staged file not removed after publish")
	}
	ims, err := st.Q().ListImagesByEntry(ctx, "a500")
	if err != nil || len(ims) != 1 || ims[0].Credit != "Photo: Someone, CC BY 4.0" || ims[0].SourceURL != srv.URL+"/redirect" ||
		ims[0].SourcePage != "https://example.org/page" || ims[0].Width != 4 {
		t.Errorf("images rows = %+v, %v", ims, err)
	}

	t.Run("undo", func(t *testing.T) {
		// Identical content already published: Undo keeps the existing file.
		again, err := f.Stage(ctx, srv.URL+"/img")
		if err != nil {
			t.Fatal(err)
		}
		dup, err := f.Publish(again, "a500", "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := dup.Undo(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(cfg.MediaDir(), strings.TrimPrefix(path, "images/"))); err != nil {
			t.Errorf("undo of a pre-existing file removed it: %v", err)
		}
		// A file this call moved is removed again.
		fresh, err := f.Stage(ctx, srv.URL+"/img")
		if err != nil {
			t.Fatal(err)
		}
		other, err := f.Publish(fresh, "a1200", "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := other.Undo(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(cfg.MediaDir(), strings.TrimPrefix(other.Path, "images/"))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("undone file still present: %v", err)
		}
	})
}

func TestFetcher_Stage_Rejects(t *testing.T) {
	t.Parallel()
	pngOK := pngBytes(t, 2, 2)
	tests := []struct {
		name          string
		allowLoopback bool
		path          string
		handler       http.HandlerFunc
		want          error
	}{
		{
			name: "loopback host", path: "/img",
			handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(pngOK) },
			want:    ErrBlockedHost,
		},
		{
			name: "redirect to metadata address", allowLoopback: true, path: "/r",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
			},
			want: ErrBlockedHost,
		},
		{
			name: "redirect to private address", allowLoopback: true, path: "/r",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://10.0.0.1/img.png", http.StatusFound)
			},
			want: ErrBlockedHost,
		},
		{
			name: "redirect to file scheme", allowLoopback: true, path: "/r",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
			},
			want: ErrURL,
		},
		{
			name: "oversized by content-length", allowLoopback: true, path: "/big",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(append(pngOK, make([]byte, MaxBytes)...))
			},
			want: ErrTooLarge,
		},
		{
			name: "oversized streamed", allowLoopback: true, path: "/big",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.(http.Flusher).Flush() // no Content-Length: chunked
				_, _ = w.Write(pngOK)
				chunk := make([]byte, 1<<20)
				for range 9 {
					_, _ = w.Write(chunk)
				}
			},
			want: ErrTooLarge,
		},
		{
			name: "svg", allowLoopback: true, path: "/x.svg",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/svg+xml")
				_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
			},
			want: ErrType,
		},
		{
			name: "html", allowLoopback: true, path: "/x.png",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte(`<!doctype html><html><body>hi</body></html>`))
			},
			want: ErrType,
		},
		{
			name: "too many pixels", allowLoopback: true, path: "/huge.png",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(pngHeader(5000, 4000))
			},
			want: ErrTooManyPx,
		},
		{
			name: "truncated png", allowLoopback: true, path: "/t.png",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(pngOK[:20])
			},
			want: ErrType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, _, cfg := newTestFetcher(t, tt.allowLoopback)
			srv := serve(t, tt.handler)
			_, err := f.Stage(t.Context(), srv.URL+tt.path)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if entries, _ := os.ReadDir(cfg.StagingDir()); len(entries) != 0 {
				t.Errorf("rejected image left %d staged files", len(entries))
			}
		})
	}
}

func TestFetcher_Stage_TooManyRedirects(t *testing.T) {
	t.Parallel()
	f, _, _ := newTestFetcher(t, true)
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	})
	if _, err := f.Stage(t.Context(), srv.URL+"/r"); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Errorf("err = %v, want too many redirects", err)
	}
}

func TestFetcher_Stage_RetriesAfter429(t *testing.T) {
	t.Parallel()
	f, _, _ := newTestFetcher(t, true)
	f.retryWait = 10 * time.Millisecond
	body := pngBytes(t, 4, 3)
	var hits atomic.Int32
	var agents []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent"))
		if hits.Add(1) == 1 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(body)
	})
	staged, err := f.Stage(t.Context(), srv.URL+"/img.png")
	if err != nil {
		t.Fatalf("Stage after one 429: %v", err)
	}
	if hits.Load() != 2 || staged.Width != 4 {
		t.Errorf("hits %d, staged %+v", hits.Load(), staged)
	}
	if agents[0] != userAgent {
		t.Errorf("User-Agent %q without a base URL", agents[0])
	}

	// A second 429 or 503 is not retried again.
	hits.Store(0)
	srv2 := serve(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "0")
		http.Error(w, "busy", http.StatusServiceUnavailable)
	})
	if _, err := f.Stage(t.Context(), srv2.URL+"/img.png"); err == nil || !strings.Contains(err.Error(), "HTTP 503") || hits.Load() != 2 {
		t.Errorf("err %v after %d requests", err, hits.Load())
	}
}

func TestFetcher_UserAgentNamesBaseURL(t *testing.T) {
	t.Parallel()
	f := New(config.Config{DataDir: t.TempDir(), BaseURL: "https://amiga.example"}, nil, t.TempDir())
	if want := "AmigaConstellation-ImageFetcher/1.0 (+https://amiga.example)"; f.userAgent != want {
		t.Errorf("User-Agent %q, want %q", f.userAgent, want)
	}
}

func TestRetryDelay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		h    string
		want time.Duration
	}{
		{"", 2 * time.Second},
		{"3", 3 * time.Second},
		{"0", 0},
		{"120", MaxRetryWait},
		{"soon", 2 * time.Second},
		{now.Add(5 * time.Second).Format(http.TimeFormat), 5 * time.Second},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{now.Add(time.Hour).Format(http.TimeFormat), MaxRetryWait},
	} {
		if got := retryDelay(tc.h, RetryWait, now); got != tc.want {
			t.Errorf("retryDelay(%q) = %v, want %v", tc.h, got, tc.want)
		}
	}
}
