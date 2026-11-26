# AGENTS.md

docker-wsl-proxy serves the Docker API at `\\.\pipe\docker_engine` on Windows for a Docker daemon inside WSL, and maps the paths of Windows in bind mounts to WSL, so that the native docker cli and every other client of Windows use Docker in WSL with no Docker Desktop. Read the [README](README.md) first.

The proxy itself, which forwards the API and rewrites the bind mounts of `POST /containers/create`, is the package `github.com/felipecrs/docker-on-docker-proxy/proxy` of [docker-on-docker-proxy](https://github.com/felipecrs/docker-on-docker-proxy). Change it there, then update the requirement in `go.mod` here.

## Layout

The command is `cmd/docker-wsl-proxy`, a `main` package for Windows only:

- `main.go`: the command line, `--distro`, `--debug` and `--version`
- `daemon_windows.go`: serves the proxy at the pipe, with the mapping of paths and the transport to WSL
- `wsl.go`: the mapping of paths of Windows to WSL, and the root of the drives from `wsl.conf`
- `wsl_windows.go`: the transport to WSL: a Hyper-V socket to a listener on vsock port 2375 in WSL, else `wsl.exe -e docker system dial-stdio` with spares waiting ahead
- `process_windows.go`: the pipe that only the current user may open, and the job object that ends the `wsl.exe` with the program
- `*_test.go`: unit tests; `e2e_*_windows_test.go`: the end to end tests

Also:

- `.github/workflows/ci.yaml`: all tests, the build, and the release; `build.yaml`: the build, called by it

## Build and test

```sh
go vet ./...
go test ./...
go build ./cmd/docker-wsl-proxy
```

The end to end tests run the proxy on a pipe of their own and a native docker cli with `DOCKER_HOST` pointed at it. They need Docker in the default WSL distribution and `DOCKER_WSL_PROXY_TEST_DOCKER` set to a `docker.exe` of Windows, with Compose and Buildx in its usual places or in `DOCKER_CONFIG`. Without them they skip, as in ci. `TestDaemon` also serves `\\.\pipe\docker_engine` when it does not exist, and `TestListenerGoneAndBack` starts and stops its own `socat` in WSL when it is installed and no listener runs.

```powershell
$env:DOCKER_WSL_PROXY_TEST_DOCKER = "C:\path\to\docker.exe"
go test -count=1 -v ./...
```

The version is printed by `docker-wsl-proxy --version`.

## Releases

Push a tag such as `v1.2.3` on `master`. ci runs all tests, and then publishes `docker-wsl-proxy-windows-amd64.zip` and `docker-wsl-proxy-windows-arm64.zip` (each with `docker-wsl-proxy.exe` and `LICENSE` at the root, as `mise use github:felipecrs/docker-wsl-proxy` expects), `checksums.txt`, and the GitHub release with generated notes. A tag with a pre-release suffix, such as `v1.2.3-rc.1`, makes a pre-release. Nothing is published from pull requests or `master`, but they run the same build.

Dependencies are updated by Renovate, see `renovate.json`.

## House rules

- The smallest complete change. Prefer removing over adding, and add no option nobody asked for.
- The standard library first. A dependency needs a reason.
- Comments say why, not what, in one line where possible.
- Every behaviour a user can see, and every limit, is written in the README with its reason.
- Pull request titles are plain sentences, as they become the commit message.
