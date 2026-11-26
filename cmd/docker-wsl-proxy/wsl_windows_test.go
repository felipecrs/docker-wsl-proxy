package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDialSkipsSparesThatEnded(t *testing.T) {
	dead, err := startStdio(exec.Command("cmd.exe", "/c", "exit 0"))
	if err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); !dead.exited(); time.Sleep(10 * time.Millisecond) {
		if time.Since(start) > 10*time.Second {
			t.Fatal("cmd.exe did not end")
		}
	}
	var started []*stdioConn
	tr := &transport{firstDone: make(chan struct{}), spares: make(chan spare, spares), probedAt: time.Now()}
	close(tr.firstDone)
	tr.dialStdio = func() (net.Conn, error) {
		// more waits for its input, as dial-stdio does.
		c, err := startStdio(exec.Command("cmd.exe", "/c", "more"))
		if err == nil {
			tr.mu.Lock()
			started = append(started, c)
			tr.mu.Unlock()
		}
		return c, err
	}
	t.Cleanup(func() {
		tr.stopSpares()
		tr.mu.Lock()
		defer tr.mu.Unlock()
		for _, c := range started {
			c.Close()
		}
	})
	// The dead one waits first, as a spare whose wsl.exe ended with WSL.
	tr.sparing, tr.stop = true, make(chan struct{})
	tr.spares <- spare{dead, nil}
	go tr.addSpare(tr.epoch)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := tr.dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if conn == net.Conn(dead) {
		t.Fatal("got the spare whose process ended")
	}
	if conn.(*stdioConn).exited() {
		t.Fatal("got a spare whose process ended")
	}
}

// The transport finds a listener in WSL, goes on through wsl.exe when it goes away, and finds it
// again. The test starts its own listener with socat, and stops exactly that process.
func TestListenerGoneAndBack(t *testing.T) {
	nativeDocker(t)
	if err := exec.Command("wsl.exe", "-e", "sh", "-c", "command -v socat").Run(); err != nil {
		t.Skip("no socat in the default WSL distribution")
	}
	if findListener() != nil {
		t.Skipf("a listener on vsock port %d runs already", vsockPort)
	}
	listen := func() (stop func()) {
		cmd := exec.Command("wsl.exe", "-e", "sh", "-c",
			fmt.Sprintf("echo $$; exec socat -t 86400 VSOCK-LISTEN:%d,fork UNIX-CONNECT:/run/docker.sock", vsockPort))
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		pid, err := bufio.NewReader(out).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		stop = func() {
			once.Do(func() {
				_ = exec.Command("wsl.exe", "-e", "kill", strings.TrimSpace(pid)).Run()
				_ = cmd.Wait()
			})
		}
		t.Cleanup(stop)
		for i := 0; findListener() == nil; i++ {
			if i == 50 {
				t.Fatal("socat did not listen")
			}
			time.Sleep(100 * time.Millisecond)
		}
		return stop
	}
	stop := listen()

	tr := newTransport(dialStdio)
	t.Cleanup(tr.stopSpares)
	<-tr.firstDone
	if tr.listener() == nil {
		t.Fatal("the listener was not found")
	}
	found := make(chan struct{}, 1)
	tr.mu.Lock()
	tr.found = append(tr.found, func() { found <- struct{}{} })
	tr.mu.Unlock()
	ping := func(throughWSLExe bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := tr.dial(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, ok := conn.(*stdioConn); ok != throughWSLExe {
			t.Errorf("a connection through wsl.exe: %v, want %v", ok, throughWSLExe)
		}
		_, _ = io.WriteString(conn, "GET /_ping HTTP/1.1\r\nHost: docker\r\n\r\n")
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("ping: %s", resp.Status)
		}
	}
	ping(false)

	stop()
	ping(true)
	if tr.listener() != nil {
		t.Error("the listener that went away is still used")
	}

	listen()
	// As if reprobeAfter passed since the last probe ended.
	for {
		tr.mu.Lock()
		probing := tr.probing
		if !probing {
			tr.probedAt = time.Time{}
		}
		tr.mu.Unlock()
		if !probing {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Answered through wsl.exe while the probe looks.
	ping(true)
	select {
	case <-found:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener was not found again")
	}
	ping(false)
}
