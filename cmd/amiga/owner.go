package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"amigaconstellation/internal/auth"
	"amigaconstellation/internal/config"
)

// ownerCmd is the local recovery command for the single owner account.
// Credentials come from AMIGA_OWNER_USER / AMIGA_OWNER_PASSWORD when set
// (non-interactive use), otherwise from prompts on stdin.
func ownerCmd(ctx context.Context, cfg config.Config, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return errUsage
	}
	st, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := auth.New(st, cfg, time.Now)
	in := bufio.NewReader(stdin)

	switch args[0] {
	case "init":
		user := strings.TrimSpace(getenv("AMIGA_OWNER_USER"))
		if user == "" {
			if user, err = prompt(in, stderr, "Username: "); err != nil {
				return err
			}
		}
		if err := auth.CheckUsername(user); err != nil {
			return err
		}
		pw, err := newPassword(in, getenv, stdin, stderr)
		if err != nil {
			return err
		}
		en, err := svc.InitOwner(ctx, user, pw)
		if errors.Is(err, auth.ErrOwnerExists) {
			return fmt.Errorf("%w; use `amiga owner reset-password`, `reset-totp` or `recovery-codes`", err)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Owner %q created.\n\n", user)
		printTOTP(stdout, en)
		printCodes(stdout, en.RecoveryCodes)
		return nil

	case "reset-password":
		pw, err := newPassword(in, getenv, stdin, stderr)
		if err != nil {
			return err
		}
		if err := svc.ResetPassword(ctx, pw); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Password changed. Every session was signed out.")
		return nil

	case "reset-totp":
		en, err := svc.ResetTOTP(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Authenticator secret replaced. Every session was signed out; the old codes no longer work.")
		fmt.Fprintln(stdout)
		printTOTP(stdout, en)
		return nil

	case "unlock":
		if err := svc.UnlockLogins(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Sign-in throttle cleared; every recorded failed login was removed.")
		return nil

	case "recovery-codes":
		codes, err := svc.RegenerateRecoveryCodes(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Recovery codes replaced; the old ones no longer work.")
		fmt.Fprintln(stdout)
		printCodes(stdout, codes)
		return nil
	}
	return errUsage
}

func printTOTP(w io.Writer, en auth.Enrollment) {
	fmt.Fprintln(w, "Add this account to your authenticator app (shown once):")
	fmt.Fprintln(w, "  URI:    "+en.URI)
	fmt.Fprintln(w, "  Secret: "+en.Secret)
	fmt.Fprintln(w)
}

func printCodes(w io.Writer, codes []string) {
	fmt.Fprintln(w, "Recovery codes (each works once in the code field; store them offline, shown once):")
	for _, c := range codes {
		fmt.Fprintln(w, "  "+c)
	}
}

// newPassword reads AMIGA_OWNER_PASSWORD or prompts twice without echo.
func newPassword(in *bufio.Reader, getenv func(string) string, stdin io.Reader, stderr io.Writer) (string, error) {
	if pw := getenv("AMIGA_OWNER_PASSWORD"); pw != "" {
		return pw, auth.CheckPassword(pw)
	}
	restore := noEcho(stdin)
	defer restore()
	pw, err := prompt(in, stderr, fmt.Sprintf("Password (at least %d characters): ", auth.MinPasswordLen))
	fmt.Fprintln(stderr)
	if err != nil {
		return "", err
	}
	if err := auth.CheckPassword(pw); err != nil {
		return "", err
	}
	again, err := prompt(in, stderr, "Repeat password: ")
	fmt.Fprintln(stderr)
	if err != nil {
		return "", err
	}
	if again != pw {
		return "", errors.New("passwords do not match")
	}
	return pw, nil
}

func prompt(in *bufio.Reader, w io.Writer, label string) (string, error) {
	fmt.Fprint(w, label)
	line, err := in.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// noEcho turns terminal echo off while a password is typed, when stdin is
// a terminal; it returns the function that turns it back on.
func noEcho(stdin io.Reader) func() {
	f, ok := stdin.(*os.File)
	if !ok {
		return func() {}
	}
	if fi, err := f.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return func() {}
	}
	off := exec.Command("stty", "-echo")
	off.Stdin = f
	if err := off.Run(); err != nil {
		return func() {}
	}
	return func() {
		on := exec.Command("stty", "echo")
		on.Stdin = f
		_ = on.Run()
	}
}
