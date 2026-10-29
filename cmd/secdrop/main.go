// Command secdrop is a keyboard-first browser for Berglas secrets.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dumbprism19/secdrop/internal/config"
	"github.com/dumbprism19/secdrop/internal/keysetup"
	"github.com/dumbprism19/secdrop/internal/ui"
)

// version can be set by a release build with -ldflags "-X main.version=…".
// Otherwise Go's build info supplies it: `go install …@v0.3.0` reports 0.3.0,
// and a build from a checkout reports a pseudo-version for its commit.
var version = ""

func currentVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}

func main() {
	var opts ui.Options
	flag.StringVar(&opts.Bucket, "bucket", "", "open this bucket instead of the remembered one")
	flag.BoolVar(&opts.Pick, "pick", false, "choose a bucket even if one is remembered")
	flag.BoolVar(&opts.NoMotion, "no-motion", false, "turn off animation (also SECDROP_NO_MOTION=1)")
	noAutoUpdate := flag.Bool("no-auto-update", false, "don't install new releases on launch (also SECDROP_NO_AUTO_UPDATE=1)")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Parse()
	switch flag.Arg(0) {
	case "run", "export":
		run := runCmd
		if flag.Arg(0) == "export" {
			run = exportCmd
		}
		if err := run(flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "secdrop:", err)
			os.Exit(1)
		}
		return
	}
	if flag.Arg(0) == "setup-keys" {
		if err := keysetup.Run(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "secdrop:", err)
			os.Exit(1)
		}
		return
	}
	if *showVersion {
		fmt.Println("secdrop", currentVersion())
		return
	}
	if os.Getenv("SECDROP_NO_MOTION") != "" || os.Getenv("REDUCE_MOTION") != "" {
		opts.NoMotion = true
	}
	opts.Version = currentVersion()
	autoUpdate(&opts, *noAutoUpdate || os.Getenv("SECDROP_NO_AUTO_UPDATE") != "" || config.Load().NoAutoUpdate)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := tea.NewProgram(ui.New(ctx, opts), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "secdrop:", err)
		os.Exit(1)
	}
}
