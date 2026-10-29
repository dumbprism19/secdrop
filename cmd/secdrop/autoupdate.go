package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/dumbprism19/secdrop/internal/config"
	"github.com/dumbprism19/secdrop/internal/ui"
	"github.com/dumbprism19/secdrop/internal/update"
)

// updatedFromEnv is set on the restarted process so it can say what changed
// and doesn't try to update again.
const updatedFromEnv = "SECDROP_UPDATED_FROM"

// autoUpdate runs before the app opens. If a newer release exists and this
// copy was installed with `go install`, it installs the new one and restarts
// into it. Anything that stops that (offline, turned off, installed another
// way, install failed) leaves a notice for the home screen instead.
func autoUpdate(opts *ui.Options, disabled bool) {
	if from := os.Getenv(updatedFromEnv); from != "" {
		os.Unsetenv(updatedFromEnv)
		opts.UpdatedFrom = from
		return
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

	exe, ok := update.SelfInstallable()
	if disabled || !ok {
		return
	}

	fmt.Fprintf(os.Stderr, "Updating secdrop %s → %s…\n", current, latest[1:])
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	err = update.Install(ctx, latest)
	cancel()
	if err != nil {
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
