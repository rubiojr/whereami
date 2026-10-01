// Command wayland-present runs a native Wayland client and reports when the
// compositor actually presented its frames, from wp_presentation feedback on
// each surface commit. Unlike render-callback or swap timings, these are the
// compositor's display timestamps. It works for any Wayland client, so the
// vecmap viewer and MapLibre Native can be measured the same way.
//
// Usage:
//
//	QT_QPA_PLATFORM=wayland wayland-present [flags] -- command [args...]
//
// X11 clients, including Qt's xcb platform under Xwayland, are not supported.
package main

import (
	"cmp"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	logPath := flag.String("log", "", "keep the raw feedback log at this path")
	warmup := flag.Int("warmup", 30, "presented frames to skip before sampling")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [flags] -- command [args...]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 || *warmup < 0 {
		flag.Usage()
		os.Exit(2)
	}
	err := run(flag.Args(), *logPath, *warmup, os.Stdout, os.Stderr)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run reports presentation even when the client fails, then returns its error.
func run(command []string, logPath string, warmup int, stdout, stderr io.Writer) error {
	dir, err := os.MkdirTemp("", "wayland-present-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	library, err := buildPreload(dir)
	if err != nil {
		return fmt.Errorf("build preload: %w", err)
	}
	logPath = cmp.Or(logPath, filepath.Join(dir, "present.log"))
	if err := os.Remove(logPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	preload := library
	if existing := os.Getenv("LD_PRELOAD"); existing != "" {
		preload += ":" + existing
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	cmd.Env = append(os.Environ(), "LD_PRELOAD="+preload, "WHEREAMI_PRESENT_LOG="+logPath)
	runErr := cmd.Run()
	data, err := os.ReadFile(logPath)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.Join(runErr, errors.New("the client never connected to a Wayland display; run Qt clients with QT_QPA_PLATFORM=wayland"))
	}
	if err != nil {
		return errors.Join(runErr, err)
	}
	log, err := parseLog(data)
	if err != nil {
		return errors.Join(runErr, fmt.Errorf("presentation log: %w", err))
	}
	if !log.supported {
		return errors.Join(runErr, errors.New("the compositor doesn't support wp_presentation"))
	}
	summary, err := summarize(log, warmup)
	if err != nil {
		return errors.Join(runErr, err)
	}
	summary.print(stdout)
	return runErr
}

//go:embed preload/present.c
var preloadSource []byte

// buildPreload compiles the recorder with wayland-scanner's presentation-time
// code into dir and returns the library path. CC selects the C compiler.
func buildPreload(dir string) (string, error) {
	datadir, err := exec.Command("pkg-config", "--variable=pkgdatadir", "wayland-protocols").Output()
	if err != nil {
		return "", fmt.Errorf("pkg-config wayland-protocols: %w", err)
	}
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "wayland-client").Output()
	if err != nil {
		return "", fmt.Errorf("pkg-config wayland-client: %w", err)
	}
	protocol := filepath.Join(strings.TrimSpace(string(datadir)), "stable", "presentation-time", "presentation-time.xml")
	source := filepath.Join(dir, "present.c")
	if err := os.WriteFile(source, preloadSource, 0o600); err != nil {
		return "", err
	}
	code := filepath.Join(dir, "presentation-time-protocol.c")
	library := filepath.Join(dir, "libwayland-present.so")
	compile := append(strings.Fields(cmp.Or(os.Getenv("CC"), "gcc")), "-shared", "-fPIC", "-O2", "-Wall", "-I", dir, "-o", library, source, code)
	compile = append(append(compile, strings.Fields(string(flags))...), "-ldl", "-pthread")
	for _, step := range [][]string{
		{"wayland-scanner", "client-header", protocol, filepath.Join(dir, "presentation-time-client-protocol.h")},
		{"wayland-scanner", "private-code", protocol, code},
		compile,
	} {
		if output, err := exec.Command(step[0], step[1:]...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s: %w\n%s", step[0], err, output)
		}
	}
	return library, nil
}
