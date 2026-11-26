package main

import (
	"testing"
)

func mnt() string { return "/mnt/" }

func TestWSLPath(t *testing.T) {
	tests := []struct{ in, want string }{
		{`C:\a b`, "/mnt/c/a b"},
		{"D:/a", "/mnt/d/a"},
		{`c:\`, "/mnt/c/"},
		{`\\wsl.localhost\Ubuntu\home\me`, "/home/me"},
		{`\\wsl$\Ubuntu\home\me`, "/home/me"},
		{`//WSL.localhost/Ubuntu`, "/"},
		{"/srv", "/srv"},
		{"cache", "cache"},
		{`\\server\share\dir`, `\\server\share\dir`},
		{"C:", "C:"},
	}
	for _, tt := range tests {
		if got := wslPath(tt.in, mnt); got != tt.want {
			t.Errorf("wslPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := wslPath(`E:\x`, func() string { return "/" }); got != "/e/x" {
		t.Errorf("with root /: %q", got)
	}
	if got := wslPath("/srv", func() string { t.Error("root read for a path of Linux"); return "" }); got != "/srv" {
		t.Errorf("got %q", got)
	}
}

func TestAutomountRoot(t *testing.T) {
	tests := []struct{ conf, want string }{
		{"", "/mnt/"},
		{"[boot]\nsystemd = true\n", "/mnt/"},
		{"[automount]\nroot = /\n", "/"},
		{"[automount]\r\nenabled = true\r\nroot = /windir/ # drives\r\n", "/windir/"},
		{"[Automount]\nRoot=\"/x\"\n", "/x"},
		{"[network]\nroot = /no/\n[automount]\noptions = \"metadata\"\n", "/mnt/"},
		{"[automount]\n# root = /commented/\n", "/mnt/"},
	}
	for _, tt := range tests {
		if got := automountRoot(tt.conf); got != tt.want {
			t.Errorf("automountRoot(%q) = %q, want %q", tt.conf, got, tt.want)
		}
	}
}
