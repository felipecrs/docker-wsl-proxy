package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestListenPipeRefusesAPipeThatExists(t *testing.T) {
	path := `\\.\pipe\docker-wsl-proxy-test-` + filepath.Base(t.TempDir())
	listener, err := listenPipe(path)
	if err != nil {
		t.Fatal(err)
	}
	if !pipeExists(path) {
		t.Error("the pipe is not there")
	}
	if second, err := listenPipe(path); err == nil {
		second.Close()
		t.Error("listened on a pipe that exists")
	}
	listener.Close()
	if pipeExists(path) {
		t.Error("the pipe is still there")
	}
}

// pipeExists tells whether a named pipe exists. A pipe whose instances are busy cannot be opened.
func pipeExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}
