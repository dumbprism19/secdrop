package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"sort"
	"syscall"

	"github.com/dumbprism19/secdrop/internal/config"
	"github.com/dumbprism19/secdrop/internal/dotenv"
	"github.com/dumbprism19/secdrop/internal/secret"
	"github.com/dumbprism19/secdrop/internal/store"
)

const usageRun = `usage: secdrop run [--bucket NAME] <service> <env> -- <command> [args…]

Starts <command> with the service's secrets added to its environment.
Nothing is written to disk. Values from the secret win over ones already set.

  secdrop run payments-api dev -- npm start`

const usageExport = `usage: secdrop export [--bucket NAME] [-o FILE] <service> <env>

Prints the env file, or writes it to FILE readable only by you.
FILE is never overwritten.

  secdrop export payments-api dev -o .env.local`

// fetch decrypts one service's env from the remembered (or given) bucket.
func fetch(bucket, service, env string) (store.Secret, secret.Value, error) {
	if bucket == "" {
		bucket = config.Load().Bucket
	}
	if bucket == "" {
		return store.Secret{}, secret.Value{}, errors.New("no bucket chosen yet; open secdrop once or pass --bucket")
	}
	ctx := context.Background()
	st, err := store.New(ctx)
	if err != nil {
		return store.Secret{}, secret.Value{}, fmt.Errorf("can't reach Google Cloud (try `gcloud auth login --update-adc`): %w", err)
	}
	secs, err := st.List(ctx, bucket)
	if err != nil {
		return store.Secret{}, secret.Value{}, err
	}
	sec, err := store.FindSecret(secs, service, env)
	if err != nil {
		return store.Secret{}, secret.Value{}, err
	}
	sec, val, err := st.Read(ctx, sec)
	return sec, val, err
}

// flagsFirst moves flags (and their values) ahead of positional arguments,
// so "export svc dev -o file" works like "export -o file svc dev". For run,
// everything after "--" belongs to the command and is left alone.
func flagsFirst(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			if !containsByte(a, '=') && i+1 < len(args) && (a == "-o" || a == "--o" || a == "-bucket" || a == "--bucket") {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}

func containsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

func runCmd(args []string) error {
	fl := flag.NewFlagSet("run", flag.ContinueOnError)
	bucket := fl.String("bucket", "", "bucket to read from")
	fl.Usage = func() { fmt.Fprintln(os.Stderr, usageRun) }
	if err := fl.Parse(flagsFirst(args)); err != nil {
		return err
	}
	rest := fl.Args()
	split := -1
	for i, a := range rest {
		if a == "--" {
			split = i
			break
		}
	}
	if split != 2 || split+1 >= len(rest) {
		fl.Usage()
		return errors.New("need <service> <env> -- <command>")
	}
	sec, val, err := fetch(*bucket, rest[0], rest[1])
	if err != nil {
		return err
	}
	doc := dotenv.Parse(val.Bytes())
	if !doc.IsEnv() {
		return fmt.Errorf("%s isn't a KEY=value file, so it can't be loaded as environment variables", sec.Object)
	}

	env := map[string]string{}
	for _, kv := range os.Environ() {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				env[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	for _, e := range doc.Entries() {
		env[e.Key] = e.Value.Reveal()
	}
	list := make([]string, 0, len(env))
	for k, v := range env {
		list = append(list, k+"="+v)
	}
	sort.Strings(list)

	cmd := rest[split+1:]
	path, err := exec.LookPath(cmd[0])
	if err != nil {
		return err
	}
	// Replace this process so signals and the exit code belong to the command.
	return syscall.Exec(path, cmd, list)
}

func exportCmd(args []string) error {
	fl := flag.NewFlagSet("export", flag.ContinueOnError)
	bucket := fl.String("bucket", "", "bucket to read from")
	out := fl.String("o", "", "write to this file instead of printing")
	fl.Usage = func() { fmt.Fprintln(os.Stderr, usageExport) }
	if err := fl.Parse(flagsFirst(args)); err != nil {
		return err
	}
	if fl.NArg() != 2 {
		fl.Usage()
		return errors.New("need <service> <env>")
	}
	_, val, err := fetch(*bucket, fl.Arg(0), fl.Arg(1))
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = os.Stdout.Write(val.Bytes())
		return err
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists; choose another name", *out)
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(val.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Wrote %s (only you can read it). Don't commit it.\n", *out)
	return nil
}
