// Command amiga runs the Amiga Constellation server and its admin tasks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"amigaconstellation/internal/auth"
	"amigaconstellation/internal/catalog"
	"amigaconstellation/internal/config"
	"amigaconstellation/internal/images"
	"amigaconstellation/internal/store"
	"amigaconstellation/internal/suggest"
	"amigaconstellation/internal/turnstile"
	"amigaconstellation/internal/web"
)

const usage = `usage: amiga <command> [arguments]

commands:
  serve                     run the web server (migrates and seeds an empty database)
  import [--replace] <file> load a catalogue JSON file; --replace overwrites a non-empty catalogue
  export [file]             write the live catalogue as JSON (stdout by default)
  backup <dest-dir>         write amiga.db (VACUUM INTO), media/ and the dev secret into dest-dir
  owner init                create the owner account; prints the TOTP secret and recovery codes once
  owner reset-password      set a new owner password (signs out every session)
  owner reset-totp          replace the authenticator secret (signs out every session)
  owner recovery-codes      replace the recovery codes
  owner unlock              clear failed sign-in attempts (lifts the login throttle)

configuration is read from AMIGA_* environment variables; see ARCHITECTURE.md.
`

// errUsage marks command-line mistakes (exit status 2).
var errUsage = errors.New("usage")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(os.Stderr, "amiga:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	case "serve", "import", "export", "backup", "owner":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		return errUsage
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger := newLogger(cfg, stderr)

	switch cmd {
	case "serve":
		return serve(ctx, cfg, logger)
	case "import":
		return importCmd(ctx, cfg, rest, stdout, stderr)
	case "export":
		return exportCmd(ctx, cfg, rest, stdout)
	case "owner":
		return ownerCmd(ctx, cfg, rest, getenv, os.Stdin, stdout, stderr)
	default:
		return backupCmd(ctx, cfg, rest, stdout)
	}
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	if cfg.Production() {
		return slog.New(slog.NewJSONHandler(w, nil))
	}
	return slog.New(slog.NewTextHandler(w, nil))
}

// openStore opens and migrates the database in the data dir.
func openStore(ctx context.Context, cfg config.Config) (*store.Store, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(ctx); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("migrating: %w", err)
	}
	return st, nil
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	if cfg.Production() && !cfg.TrustProxyForwarding {
		logger.Warn("AMIGA_TRUST_PROXY is not 1: behind a reverse proxy, rate limits and the login throttle see only the proxy's address")
	}

	cat := catalog.New(st)
	res, err := cat.Seed(ctx, cfg.SeedJSON, cfg.SeedImages, cfg.MediaDir())
	if err != nil {
		return err
	}
	if res.Seeded {
		logger.Info("seeded catalogue", "entries", res.Entries, "links", res.Links, "images", res.Images)
	}

	// Feature options; each feature appends its own.
	var opts []web.Option
	verifier, widget := turnstile.FromConfig(cfg, logger)
	opts = append(opts, web.WithSuggest(suggest.New(st, cat, cfg, time.Now), verifier, widget))
	opts = append(opts, web.WithAdmin(auth.New(st, cfg, time.Now), images.New(cfg, st, cfg.MediaDir())))

	srv, err := web.New(cfg, st, cat, logger, opts...)
	if err != nil {
		return err
	}
	defer srv.Close()
	return srv.ListenAndServe(ctx)
}

func importCmd(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	fl := flag.NewFlagSet("import", flag.ContinueOnError)
	fl.SetOutput(stderr)
	replace := fl.Bool("replace", false, "overwrite a non-empty catalogue")
	if err := fl.Parse(args); err != nil {
		return errUsage
	}
	if fl.NArg() != 1 {
		return errUsage
	}
	doc, err := catalog.LoadDocument(fl.Arg(0))
	if err != nil {
		return err
	}
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	cat := catalog.New(st)
	var stats catalog.ImportStats
	if *replace {
		stats, err = cat.ImportReplace(ctx, doc, "cli")
	} else {
		stats, err = cat.Import(ctx, doc, "cli")
	}
	if errors.Is(err, catalog.ErrNotEmpty) {
		return fmt.Errorf("%w; use import --replace to overwrite it", err)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "imported: %d created, %d updated, %d restored, %d deleted, %d unchanged\n",
		stats.Created, stats.Updated, stats.Restored, stats.Deleted, stats.Unchanged)
	fmt.Fprintln(stdout, "restart a running server to refresh its catalogue cache")
	return nil
}

func exportCmd(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) > 1 {
		return errUsage
	}
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	doc, err := catalog.New(st).Export(ctx)
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "-" {
		return doc.Encode(stdout, true)
	}
	return writeFileAtomic(args[0], func(w io.Writer) error { return doc.Encode(w, true) })
}

// writeFileAtomic writes via a temp file in the same dir, then renames.
func writeFileAtomic(path string, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".export-*")
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	tmp := f.Name()
	if err := write(f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func backupCmd(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errUsage
	}
	dest := args[0]
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("creating backup dir: %w", err)
	}
	dbDest := filepath.Join(dest, "amiga.db")
	mediaDest := filepath.Join(dest, "media")
	for _, p := range []string{dbDest, mediaDest} {
		if _, err := os.Lstat(p); err == nil {
			return fmt.Errorf("backup target %s already exists", p)
		}
	}

	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.BackupTo(ctx, dbDest); err != nil {
		return err
	}
	// The development secret signs cookies and form tokens; without it a
	// restore on a fresh host invalidates every visitor id. Production keeps
	// the secret in the environment, so the file is simply absent there.
	if secret, err := os.ReadFile(filepath.Join(cfg.DataDir, "secret")); err == nil {
		if err := os.WriteFile(filepath.Join(dest, "secret"), secret, 0o600); err != nil {
			return fmt.Errorf("copying secret: %w", err)
		}
	}

	media, err := os.OpenRoot(cfg.MediaDir())
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(stdout, "backed up database to %s (no media dir yet)\n", dbDest)
		return nil
	case err != nil:
		return fmt.Errorf("opening media dir: %w", err)
	}
	defer media.Close()
	if err := os.CopyFS(mediaDest, media.FS()); err != nil {
		return fmt.Errorf("copying media: %w", err)
	}
	fmt.Fprintf(stdout, "backed up database to %s and media to %s\n", dbDest, mediaDest)
	return nil
}
