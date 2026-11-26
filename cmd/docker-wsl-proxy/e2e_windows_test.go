package main

// The end to end tests of docker-wsl-shim, run with a native docker CLI through this proxy. They
// need Docker in the default WSL distribution, and DOCKER_WSL_PROXY_TEST_DOCKER set to a docker.exe
// of Windows. Compose and Buildx come from the docker CLI's usual places, or from DOCKER_CONFIG.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const (
	image         = "alpine"
	testDockerEnv = "DOCKER_WSL_PROXY_TEST_DOCKER"
)

func TestMain(m *testing.M) {
	// Ignoring Ctrl+C is inherited, and these tests may have been started that way: then no process
	// in the terminals of the tests would get it.
	_, _, _ = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler").Call(0, 0)
	os.Exit(m.Run())
}

// nativeDocker returns the docker CLI of Windows for the test, or skips it.
func nativeDocker(t *testing.T) string {
	t.Helper()
	docker := os.Getenv(testDockerEnv)
	if docker == "" {
		t.Skip(testDockerEnv + " does not name a docker CLI of Windows")
	}
	if err := exec.Command("wsl.exe", "-e", "docker", "info").Run(); err != nil {
		t.Skip("no Docker in the default WSL distribution")
	}
	return docker
}

// startProxy serves the proxy on a pipe of its own for the test, points DOCKER_HOST at it, and
// returns the docker CLI of Windows, or skips the test.
func startProxy(t *testing.T) string {
	t.Helper()
	docker := nativeDocker(t)
	name := "docker-wsl-proxy-test-" + filepath.Base(t.TempDir())
	stop, done := make(chan os.Signal, 1), make(chan struct{})
	go func() {
		defer close(done)
		if _, err := daemon(`\\.\pipe\`+name, stop); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(func() {
		stop <- os.Interrupt
		<-done
	})
	for i := 0; !pipeExists(`\\.\pipe\` + name); i++ {
		if i == 100 {
			t.Fatal("the proxy did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Setenv("DOCKER_HOST", "npipe:////./pipe/"+name)
	return docker
}

// output is written to by a command and read by the test.
type output struct {
	mu   sync.Mutex
	text strings.Builder
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.text.Write(p)
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.text.String()
}

func TestEndToEnd(t *testing.T) {
	docker := startProxy(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello world.txt"), []byte("from windows"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		cmd := exec.Command(docker, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SHIM_TEST=a b $c")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %q: %v\n%s", args, err, out)
		}
		return strings.TrimRight(string(out), "\r\n")
	}
	expect := func(t *testing.T, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}

	t.Run("volume", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "-v", dir+":/data:ro", image, "cat", "/data/hello world.txt"), "from windows")
	})
	t.Run("volume with forward slashes", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "-v", filepath.ToSlash(dir)+":/data", image, "cat", "/data/hello world.txt"), "from windows")
	})
	t.Run("mount", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "--mount", "type=bind,src="+dir+",dst=/data", image, "cat", "/data/hello world.txt"), "from windows")
	})
	t.Run("relative volume uses the working directory", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "-v", `.\:/data`, image, "cat", "/data/hello world.txt"), "from windows")
	})
	t.Run("relative mount", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "--mount", `type=bind,src=.\,dst=/data,readonly`, image, "cat", "/data/hello world.txt"), "from windows")
	})
	t.Run("cp and exec", func(t *testing.T) {
		const name = "docker-wsl-proxy-test-cp"
		t.Cleanup(func() { _ = exec.Command(docker, "rm", "-f", name).Run() })
		run(t, "run", "-d", "--name", name, image, "sleep", "60")
		run(t, "cp", filepath.Join(dir, "hello world.txt"), name+":/in.txt")
		expect(t, run(t, "exec", name, "cat", "/in.txt"), "from windows")
		run(t, "exec", name, "sh", "-c", "echo from linux > /out.txt")
		run(t, "cp", name+":/out.txt", `.\out.txt`)
		if got, err := os.ReadFile(filepath.Join(dir, "out.txt")); err != nil || string(got) != "from linux\n" {
			t.Errorf("got %q %v", got, err)
		}
	})
	t.Run("build and compose", func(t *testing.T) {
		compose := "services:\n  app:\n    build:\n      context: .\n    image: docker-wsl-proxy-test-image\n    environment: [\"V=${SHIM_TEST}\"]\n    volumes: [\".:/data\"]\n"
		files := map[string]string{"compose.yml": compose, "Dockerfile": "FROM " + image + "\nCOPY [\"hello world.txt\", \"/copied.txt\"]\n"}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() { _ = exec.Command(docker, "rmi", "-f", "docker-wsl-proxy-test-image").Run() })
		run(t, "build", "-q", "-t", "docker-wsl-proxy-test-image", "-f", filepath.Join(dir, "Dockerfile"), dir)
		expect(t, run(t, "run", "--rm", "docker-wsl-proxy-test-image", "cat", "/copied.txt"), "from windows")
		t.Cleanup(func() {
			_ = exec.Command(docker, "compose", "-p", "docker-wsl-proxy-test", "-f", filepath.Join(dir, "compose.yml"), "down").Run()
		})
		// A relative path in the compose file, and a relative compose file.
		got := run(t, "compose", "--progress", "quiet", "-p", "docker-wsl-proxy-test", "-f", `.\compose.yml`, "run", "--rm", "-T", "app", "sh", "-c", "echo $V; cat '/data/hello world.txt'")
		if !strings.HasSuffix(got, "a b $c\nfrom windows") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("exit code and separate streams", func(t *testing.T) {
		cmd := exec.Command(docker, "run", "--rm", image, "sh", "-c", "echo out; echo err >&2; exit 37")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		_ = cmd.Run()
		if code := cmd.ProcessState.ExitCode(); code != 37 || stdout.String() != "out\n" || stderr.String() != "err\n" {
			t.Errorf("code %d, stdout %q, stderr %q", code, &stdout, &stderr)
		}
	})
	t.Run("stdin, binary safe, ends on EOF", func(t *testing.T) {
		data := make([]byte, 1<<20)
		for i := range data {
			data[i] = byte(i * 7)
		}
		cmd := exec.Command(docker, "run", "--rm", "-i", image, "cat")
		cmd.Stdin = bytes.NewReader(data)
		out, err := cmd.Output()
		if err != nil || !bytes.Equal(out, data) {
			t.Errorf("%d bytes came back, err %v", len(out), err)
		}
	})
	t.Run("output after the end of stdin", func(t *testing.T) {
		cmd := exec.Command(docker, "run", "--rm", "-i", image, "sh", "-c", "cat; sleep 2; echo ' after'")
		cmd.Stdin = strings.NewReader("in")
		if out, err := cmd.Output(); err != nil || string(out) != "in after\n" {
			t.Errorf("got %q %v", out, err)
		}
	})
	t.Run("a path that does not exist is not created on Windows", func(t *testing.T) {
		missing := filepath.Join(dir, "missing")
		cmd := exec.Command(docker, "run", "--rm", "--mount", "type=bind,src="+missing+",dst=/m", image, "true")
		if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "/mnt/") {
			t.Errorf("err %v: %s", err, out)
		}
		if _, err := os.Stat(missing); err == nil {
			t.Error("created")
		}
	})
}

// The program serves the default pipe, for clients of Windows with no DOCKER_HOST.
func TestDaemon(t *testing.T) {
	docker := nativeDocker(t)
	if pipeExists(enginePipe) {
		t.Skip(enginePipe + " exists already")
	}
	exe := filepath.Join(t.TempDir(), "docker-wsl-proxy.exe")
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	start := func() (*exec.Cmd, *output) {
		cmd, out := exec.Command(exe), &output{}
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return cmd, out
	}
	daemon, out := start()
	for i := 0; !strings.Contains(out.String(), "serving at"); i++ {
		if i == 100 {
			t.Fatalf("did not start: %q", out)
		}
		time.Sleep(50 * time.Millisecond)
	}

	second, secondOut := start()
	_ = second.Wait()
	if code := second.ProcessState.ExitCode(); code != 1 || !strings.Contains(secondOut.String(), "cannot serve") {
		t.Errorf("a second daemon: exit code %d, %q", code, secondOut)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("through the daemon"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(docker, "run", "--rm", "-v", dir+":/d", image, "cat", "/d/f.txt")
	cmd.Env = append(os.Environ(), "DOCKER_HOST=")
	if got, err := cmd.CombinedOutput(); err != nil || string(got) != "through the daemon" {
		t.Errorf("got %q %v", got, err)
	}

	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(daemon.Process.Pid)); err != nil {
		t.Fatal(err)
	}
	_ = daemon.Wait()
	if code := daemon.ProcessState.ExitCode(); code != 0 {
		t.Errorf("exit code %d, %q", code, out)
	}
	if pipeExists(enginePipe) {
		t.Error(enginePipe + " is still there")
	}
}
