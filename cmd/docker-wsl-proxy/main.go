//go:build windows

// Command docker-wsl-proxy serves the Docker API at \\.\pipe\docker_engine for a Docker daemon in
// WSL, and maps the paths of Windows in bind mounts to WSL.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// version is set at build time.
var version = "dev"

var (
	debug  bool
	distro string // the distribution of WSL that runs Docker, or "" for the default one
)

func debugf(format string, args ...any) {
	if debug {
		fmt.Fprintf(os.Stderr, "DEBUG(docker-wsl-proxy): "+format+"\n", args...)
	}
}

// logger keeps net/http's own messages, such as a client that went away, out of the output.
func logger() *log.Logger {
	if debug {
		return log.New(os.Stderr, "DEBUG(docker-wsl-proxy): ", 0)
	}
	return log.New(io.Discard, "", 0)
}

func main() {
	flag.StringVar(&distro, "distro", "", "the WSL `distribution` that runs Docker, instead of the default one")
	flag.BoolVar(&debug, "debug", false, "print debug output")
	printVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: docker-wsl-proxy [--distro <name>] [--debug]\n       docker-wsl-proxy --version")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(flag.CommandLine.Output(), "unexpected argument: %s\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}
	if *printVersion {
		fmt.Println("docker-wsl-proxy", version)
		return
	}
	if err := killChildrenWithMe(); err != nil {
		debugf("%v", err)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	code, err := daemon(enginePipe, signals)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR(docker-wsl-proxy):", err)
	}
	os.Exit(code)
}
