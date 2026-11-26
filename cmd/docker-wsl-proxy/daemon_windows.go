package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/felipecrs/docker-on-docker-proxy/proxy"
)

// enginePipe is where Windows clients of Docker look by default.
const enginePipe = `\\.\pipe\docker_engine`

// daemon serves the proxy at pipe until a signal arrives on stop, so that clients of Windows reach
// the daemon in WSL.
func daemon(pipe string, stop <-chan os.Signal) (int, error) {
	listener, err := listenPipe(pipe)
	if err != nil {
		return 1, fmt.Errorf("cannot serve at %s, which Docker Desktop or another docker-wsl-proxy may have: %w", pipe, err)
	}
	transport := proxy.NewTransport(wslTransport().dial)
	// Pooled connections through wsl.exe would never give way to the listener under steady use.
	onListenerFound(transport.CloseIdleConnections)
	root := wslRoot()
	handler := proxy.New("docker-wsl-proxy", transport, func(source string) (string, bool, []proxy.NestedMount, error) {
		return wslPath(source, root), false, nil, nil
	}, debugf)
	fmt.Fprintf(os.Stderr, "docker-wsl-proxy %s: serving at %s, forwarding to the Docker daemon in WSL\n", version, pipe)
	served := make(chan error, 1)
	go func() { served <- (&http.Server{Handler: handler, ErrorLog: logger()}).Serve(listener) }()
	select {
	case <-stop:
		return 0, listener.Close()
	case err := <-served:
		return 1, err
	}
}
