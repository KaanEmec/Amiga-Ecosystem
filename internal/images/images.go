// Package images fetches remote images for the owner safely and publishes
// them into the media dir with provenance.
//
// Fetching is SSRF-guarded: only http(s) URLs without credentials; every
// host is resolved first and every resolved address must be public; the
// connection dials exactly the vetted address; each redirect (at most
// three) is vetted again. Bodies are capped, typed by their bytes and
// checked with the stdlib image decoders before anything is kept.
package images

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // register GIF for DecodeConfig
	_ "image/jpeg" // register JPEG
	_ "image/png"  // register PNG
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp" // register WebP

	"amigaconstellation/internal/config"
	"amigaconstellation/internal/store"
)

// Limits.
const (
	MaxBytes     = 8 << 20    // download cap
	MaxPixels    = 16_000_000 // width * height cap
	MaxRedirects = 3
	Timeout      = 10 * time.Second
	maxURLLen    = 2048
)

// Errors returned by Stage and Publish.
var (
	ErrURL         = errors.New("image URL must be an http(s) URL without credentials")
	ErrBlockedHost = errors.New("image host resolves to a non-public address")
	ErrTooLarge    = fmt.Errorf("image is larger than %d MB", MaxBytes>>20)
	ErrTooManyPx   = fmt.Errorf("image has more than %d megapixels", MaxPixels/1_000_000)
	ErrType        = errors.New("image must be JPEG, PNG, GIF or WebP")
	ErrStaged      = errors.New("staged image is missing or changed")
)

// types maps sniffed content types to the decoder name and file extension.
var types = map[string]struct{ format, ext string }{
	"image/jpeg": {"jpeg", "jpg"},
	"image/png":  {"png", "png"},
	"image/gif":  {"gif", "gif"},
	"image/webp": {"webp", "webp"},
}

var (
	entryIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	stagedPattern  = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|png|gif|webp)$`)
)

// Staged is a fetched, validated image waiting in the staging dir.
type Staged struct {
	Name        string // file name inside the staging dir
	SourceURL   string // URL as requested
	FinalURL    string // URL after redirects
	ContentType string
	Ext         string
	Width       int
	Height      int
	Bytes       int64
	SHA256      string // hex
}

// Fetcher downloads, validates, stages and publishes images. It runs one
// fetch at a time.
type Fetcher struct {
	store      *store.Store
	mediaDir   string
	stagingDir string
	client     *http.Client
	now        func() time.Time
	sem        chan struct{}

	// vet decides whether an address may be dialled (tests relax it).
	vet func(netip.Addr) error
	// lookup resolves host names (net.DefaultResolver in production).
	lookup func(ctx context.Context, host string) ([]netip.Addr, error)
}

// New builds a Fetcher that stages under cfg.StagingDir() and publishes
// into mediaDir.
func New(cfg config.Config, st *store.Store, mediaDir string, opts ...Option) *Fetcher {
	f := &Fetcher{
		store:      st,
		mediaDir:   mediaDir,
		stagingDir: cfg.StagingDir(),
		now:        time.Now,
		sem:        make(chan struct{}, 1),
		vet:        vetAddr,
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
	}
	for _, opt := range opts {
		opt(f)
	}
	dialer := &net.Dialer{Timeout: Timeout}
	transport := &http.Transport{
		Proxy: nil, // never route through an environment proxy
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			p, err := strconv.ParseUint(port, 10, 16)
			if err != nil || p == 0 {
				return nil, fmt.Errorf("invalid port %q", port)
			}
			ip, err := f.resolve(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, netip.AddrPortFrom(ip, uint16(p)).String())
		},
		TLSHandshakeTimeout:    Timeout,
		ResponseHeaderTimeout:  Timeout,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
		ForceAttemptHTTP2:      true,
	}
	f.client = &http.Client{
		Transport: transport,
		Timeout:   Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > MaxRedirects {
				return fmt.Errorf("more than %d redirects", MaxRedirects)
			}
			return checkURL(req.URL)
		},
	}
	return f
}

// Option adjusts a Fetcher.
type Option func(*Fetcher)

// WithAddrPolicy replaces the dial-address check. It exists for tests that
// fetch from a loopback httptest server; production code must not use it.
func WithAddrPolicy(vet func(netip.Addr) error) Option {
	return func(f *Fetcher) { f.vet = vet }
}

// DefaultAddrPolicy is the production dial-address check, for wrapping in
// WithAddrPolicy.
func DefaultAddrPolicy(a netip.Addr) error { return vetAddr(a) }

// OpenStaged opens a staged file by name for the owner's preview. Only
// names Stage generates are accepted, and the file is opened through an
// os.Root on the staging dir, so nothing outside it can be reached.
func (f *Fetcher) OpenStaged(name string) (*os.File, error) {
	if !stagedPattern.MatchString(name) {
		return nil, ErrStaged
	}
	root, err := os.OpenRoot(f.stagingDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStaged, err)
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStaged, err)
	}
	return file, nil
}

// resolve looks host up and returns the first address, requiring every
// returned address to pass vet (so a mixed answer cannot slip through).
func (f *Fetcher) resolve(ctx context.Context, host string) (netip.Addr, error) {
	addrs, err := f.lookup(ctx, host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolving %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("resolving %s: no addresses", host)
	}
	for _, a := range addrs {
		if err := f.vet(a.Unmap()); err != nil {
			return netip.Addr{}, err
		}
	}
	return addrs[0].Unmap(), nil
}

// blockedPrefixes are non-public ranges not covered by the netip predicates.
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",          // "this network"
		"100.64.0.0/10",      // carrier-grade NAT (also Alibaba metadata 100.100.100.200)
		"192.0.0.0/24",       // IETF protocol assignments
		"192.0.2.0/24",       // TEST-NET-1
		"198.18.0.0/15",      // benchmarking
		"198.51.100.0/24",    // TEST-NET-2
		"203.0.113.0/24",     // TEST-NET-3
		"240.0.0.0/4",        // reserved, includes broadcast
		"64:ff9b::/96",       // NAT64 can reach private IPv4
		"64:ff9b:1::/48",     // local-use NAT64
		"100::/64",           // discard-only
		"2001::/23",          // IETF protocol assignments (Teredo, ORCHID...)
		"2001:db8::/32",      // documentation
		"2002::/16",          // 6to4 can embed private IPv4
		"fec0::/10",          // deprecated site-local
		"fd00:ec2::254/128",  // AWS IPv6 metadata (also inside fc00::/7)
		"169.254.169.254/32", // cloud metadata (also link-local)
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// vetAddr rejects loopback, private, link-local, ULA, multicast,
// unspecified, metadata and other special-purpose addresses.
func vetAddr(a netip.Addr) error {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() ||
		a.IsMulticast() || !a.IsGlobalUnicast() {
		return fmt.Errorf("%w (%s)", ErrBlockedHost, a)
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return fmt.Errorf("%w (%s)", ErrBlockedHost, a)
		}
	}
	return nil
}

// checkURL enforces scheme, host and no embedded credentials.
func checkURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" ||
		u.User != nil || u.Opaque != "" {
		return ErrURL
	}
	return nil
}

// ParseURL validates a user-supplied image URL.
func ParseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLLen {
		return nil, ErrURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, ErrURL
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	return u, nil
}

// Stage downloads sourceURL, validates it as a raster image and writes it
// to the staging dir. Only one Stage runs at a time.
func (f *Fetcher) Stage(ctx context.Context, sourceURL string) (Staged, error) {
	u, err := ParseURL(sourceURL)
	if err != nil {
		return Staged{}, err
	}
	select {
	case f.sem <- struct{}{}:
		defer func() { <-f.sem }()
	case <-ctx.Done():
		return Staged{}, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Staged{}, ErrURL
	}
	req.Header.Set("User-Agent", "AmigaConstellation-ImageFetcher/1.0")
	req.Header.Set("Accept", "image/jpeg, image/png, image/gif, image/webp")
	resp, err := f.client.Do(req)
	if err != nil {
		return Staged{}, fmt.Errorf("fetching image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Staged{}, fmt.Errorf("fetching image: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > MaxBytes {
		return Staged{}, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return Staged{}, fmt.Errorf("reading image: %w", err)
	}
	if len(data) > MaxBytes {
		return Staged{}, ErrTooLarge
	}
	info, err := inspect(data)
	if err != nil {
		return Staged{}, err
	}
	info.SourceURL = u.String()
	info.FinalURL = resp.Request.URL.String()

	name, err := randomName(info.Ext)
	if err != nil {
		return Staged{}, err
	}
	if err := os.MkdirAll(f.stagingDir, 0o750); err != nil {
		return Staged{}, fmt.Errorf("creating staging dir: %w", err)
	}
	if err := writeExclusive(filepath.Join(f.stagingDir, name), data); err != nil {
		return Staged{}, err
	}
	info.Name = name
	return info, nil
}

// inspect sniffs and decodes the image header.
func inspect(data []byte) (Staged, error) {
	ct := http.DetectContentType(data)
	t, ok := types[ct]
	if !ok {
		return Staged{}, fmt.Errorf("%w (got %s)", ErrType, ct)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != t.format {
		return Staged{}, fmt.Errorf("%w (undecodable %s)", ErrType, t.format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Staged{}, fmt.Errorf("%w (empty image)", ErrType)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Staged{}, ErrTooManyPx
	}
	sum := sha256.Sum256(data)
	return Staged{
		ContentType: ct, Ext: t.ext, Width: cfg.Width, Height: cfg.Height,
		Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
	}, nil
}

// Published is a staged image moved into the media dir whose provenance
// row is not written yet. Record runs inside the catalogue transaction
// (it is a catalog.TxHook); Undo removes the file again when that
// publication fails, so a refused accept leaves nothing public behind.
type Published struct {
	Path  string // catalogue path, "images/suggested/..."
	row   store.Image
	dest  string // absolute path of the media file
	moved bool   // this call created dest (false when identical content already existed)
}

// Record inserts the image's provenance row in tx.
func (p *Published) Record(ctx context.Context, tx *sql.Tx) error {
	_, err := store.NewQueries(tx).InsertImage(ctx, p.row)
	return err
}

// Undo deletes the media file if this publication created it.
func (p *Published) Undo() error {
	if !p.moved {
		return nil
	}
	if err := os.Remove(p.dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing unpublished image: %w", err)
	}
	return nil
}

// Publish moves a staged image to media/suggested/<entry>-<sha8>.<ext>
// and returns it with its provenance row pending: the caller passes
// Record as a TxHook to the catalogue publication and calls Undo when
// that publication fails. The staged file is re-read and re-checked, so
// a Staged value round-tripped through a form cannot point elsewhere.
func (f *Fetcher) Publish(st Staged, entryID, credit, sourcePage string, suggestionID *int64) (*Published, error) {
	if !entryIDPattern.MatchString(entryID) || len(entryID) > 64 {
		return nil, fmt.Errorf("invalid entry id %q", entryID)
	}
	if !stagedPattern.MatchString(st.Name) {
		return nil, ErrStaged
	}
	if sourcePage != "" {
		if _, err := ParseURL(sourcePage); err != nil {
			return nil, fmt.Errorf("source page: %w", err)
		}
	}
	staging, err := os.OpenRoot(f.stagingDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStaged, err)
	}
	defer staging.Close()
	data, err := staging.ReadFile(st.Name)
	if err != nil || int64(len(data)) > MaxBytes {
		return nil, ErrStaged
	}
	info, err := inspect(data)
	if err != nil {
		return nil, err
	}
	if info.SHA256 != st.SHA256 || info.Ext != st.Ext {
		return nil, ErrStaged
	}

	rel := "suggested/" + entryID + "-" + info.SHA256[:8] + "." + info.Ext
	dir := filepath.Join(f.mediaDir, "suggested")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating media dir: %w", err)
	}
	dest := filepath.Join(f.mediaDir, filepath.FromSlash(rel))
	moved := true
	if _, err := os.Lstat(dest); err == nil {
		moved = false
		_ = staging.Remove(st.Name) // identical content already published
	} else if err := os.Rename(filepath.Join(f.stagingDir, st.Name), dest); err != nil {
		// Different filesystems: copy, then drop the staged file.
		if werr := writeExclusive(dest, data); werr != nil {
			return nil, fmt.Errorf("publishing image: %w", werr)
		}
		_ = staging.Remove(st.Name)
	}

	eid := entryID
	return &Published{
		Path: "images/" + rel,
		row: store.Image{
			SuggestionID: suggestionID, EntryID: &eid, SourceURL: st.SourceURL, SourcePage: sourcePage,
			Credit: credit, Path: rel, ContentType: info.ContentType, Width: info.Width, Height: info.Height,
			Bytes: info.Bytes, SHA256: info.SHA256, CreatedAt: f.now().Unix(),
		},
		dest:  dest,
		moved: moved,
	}, nil
}

// Discard deletes a staged image that will not be published.
func (f *Fetcher) Discard(st Staged) error {
	if !stagedPattern.MatchString(st.Name) {
		return ErrStaged
	}
	err := os.Remove(filepath.Join(f.stagingDir, st.Name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("discarding staged image: %w", err)
	}
	return nil
}

func randomName(ext string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("naming staged image: %w", err)
	}
	return hex.EncodeToString(b) + "." + ext, nil
}

// writeExclusive creates path (failing if it exists) with mode 0640.
func writeExclusive(path string, data []byte) error {
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	if _, err := fh.Write(data); err != nil {
		_ = fh.Close()
		_ = os.Remove(path)
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	if err := fh.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	return nil
}
