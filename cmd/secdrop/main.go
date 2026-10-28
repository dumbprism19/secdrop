// Command secdrop is a keyboard-first browser for Berglas secrets.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dumbprism19/secdrop/internal/keysetup"
	"github.com/dumbprism19/secdrop/internal/ui"
)

var version = "0.1.0"

func main() {
	var opts ui.Options
	flag.StringVar(&opts.Bucket, "bucket", "", "open this bucket instead of the remembered one")
	flag.BoolVar(&opts.Pick, "pick", false, "choose a bucket even if one is remembered")
	flag.BoolVar(&opts.NoMotion, "no-motion", false, "turn off animation (also SECDROP_NO_MOTION=1)")
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
		fmt.Println("secdrop", version)
		return
	}
	if os.Getenv("SECDROP_NO_MOTION") != "" || os.Getenv("REDUCE_MOTION") != "" {
		opts.NoMotion = true
	}
	opts.Version = version

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := tea.NewProgram(ui.New(ctx, opts), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "secdrop:", err)
		os.Exit(1)
	}
}
