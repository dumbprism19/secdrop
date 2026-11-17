package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dumbprism19/secdrop/internal/config"
	"github.com/dumbprism19/secdrop/internal/ui"
	"github.com/dumbprism19/secdrop/internal/update"
)

// channel is "release" in builds from .goreleaser.yaml (the archives that
// install.sh downloads). Those update by downloading the next release;
// everything else updates with `go install`, if it can.
var channel = ""

// updatedFromEnv is set on the restarted process so it can say what changed
// and doesn't try to update again.
const updatedFromEnv = "SECDROP_UPDATED_FROM"

// autoUpdate runs before the app opens. If a newer release exists, it
// installs it the same way this copy was installed (a release download for
// install.sh copies, `go install` otherwise) and restarts into it. Anything
// that stops that (offline, turned off, installed another way, install
// failed) leaves a notice for the home screen instead.
func autoUpdate(opts *ui.Options, disabled bool) {
	if from := os.Getenv(updatedFromEnv); from != "" {
		os.Unsetenv(updatedFromEnv)
		opts.UpdatedFrom = from
		return
	}
	fromRelease := channel == "release"
	opts.UpdateCommand = update.GoInstallCmd
	if fromRelease {
		opts.UpdateCommand = update.CurlCmd
	}
	current := opts.Version
	if !update.IsRelease(current) {
		return // development builds never update themselves
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	latest, err := update.Check(ctx, config.Dir())
	cancel()
	if err != nil || !update.Newer(current, latest) {
		return // offline or already current: start normally, quietly
	}
	opts.UpdateAvailable = latest

	if disabled {
		return
	}
	var exe string
	var install func(context.Context) error
	if fromRelease {
		e, err := os.Executable()
		if err == nil {
			e, err = filepath.EvalSymlinks(e)
		}
		if err != nil {
			opts.UpdateErr = err
			return
		}
		exe = e
		install = func(ctx context.Context) error { return update.InstallRelease(ctx, latest, exe) }
	} else {
		e, ok := update.SelfInstallable()
		if !ok {
			return
		}
		exe = e
		install = func(ctx context.Context) error { return update.Install(ctx, latest) }
	}

	fmt.Fprintf(os.Stderr, "Updating secdrop %s → %s…\n", current, latest[1:])
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = install(ctx)
	cancel()
	if errors.Is(err, update.ErrNotPublished) {
		// Tagged, but the release build hasn't finished. Try on a later launch.
		fmt.Fprintf(os.Stderr, "secdrop %s isn't ready to download yet; starting %s.\n", latest[1:], current)
		opts.UpdateAvailable = ""
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "secdrop: couldn't update to %s: %v\n", latest[1:], err)
		opts.UpdateErr = err
		return
	}

	// Start the new version in place of this one, with the same arguments.
	env := append(os.Environ(), updatedFromEnv+"="+current)
	if err := syscall.Exec(exe, os.Args, env); err != nil {
		fmt.Fprintf(os.Stderr, "Updated to %s. Run secdrop again to use it.\n", latest[1:])
		os.Exit(0)
	}
}
