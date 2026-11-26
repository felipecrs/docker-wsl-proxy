# docker-wsl-proxy

A Docker endpoint on Windows for the Docker Engine inside WSL, without Docker Desktop. It serves `\\.\pipe\docker_engine`, where programs on Windows look for Docker, and maps the Windows paths of bind mounts to WSL.

## Why?

Docker runs fine in WSL 2 without Docker Desktop, but then only WSL can reach it. Programs on Windows that talk to Docker find nothing at `\\.\pipe\docker_engine`: the docker cli for Windows, Compose, Testcontainers, the Docker support of editors and IDEs.

Giving them an endpoint is half of it. `docker run -v C:\project:/src` still sends `C:\project` to a daemon on Linux, which has no such path.

## Is it for you?

| You want | Use |
|---|---|
| Only a `docker` command on Windows, with nothing to keep running | [docker-wsl-shim](https://github.com/felipecrs/docker-wsl-shim). It runs the docker cli of WSL, so you need no docker cli for Windows. |
| Programs that talk to Docker themselves, such as Testcontainers and IDEs, or the docker cli for Windows with its plugins | docker-wsl-proxy |
| Both | Both. They do not get in each other's way. |
| A product with a user interface, Kubernetes and support | [Docker Desktop](https://www.docker.com/products/docker-desktop/), which does all of this and more. It is free for personal use, education, non-commercial open source, and companies with fewer than 250 employees and less than 10 million dollars of yearly revenue. [Everyone else pays.](https://docs.docker.com/subscription/desktop-license/) |

## Installation

1. Install Docker in your default WSL 2 distribution, with the docker cli, usable without `sudo`.
2. Install docker-wsl-proxy. With [mise](https://mise.jdx.dev):

   ```powershell
   mise use -g github:felipecrs/docker-wsl-proxy
   ```

   Or put `docker-wsl-proxy.exe` from the [latest release](https://github.com/felipecrs/docker-wsl-proxy/releases/latest) in your `PATH`.
3. Install the clients you want. For the docker cli with Compose and Buildx, for example with [Scoop](https://scoop.sh):

   ```powershell
   scoop install docker docker-compose docker-buildx
   ```

   Scoop's `docker` also brings `dockerd.exe`, which runs Windows containers only and is not needed.

## Usage

```powershell
docker-wsl-proxy
```

It serves until Ctrl+C. In another terminal:

```powershell
docker run --rm -v C:\Windows\System32\drivers\etc:/etc/windows:ro alpine cat /etc/windows/hosts
```

Options:

- `--distro <name>`: the WSL distribution that runs Docker, instead of the default one.
- `--debug`: print the connections and each path it maps.
- `--version`: print the version.

### Start it when you sign in

This creates a shortcut in your Startup folder that starts it with no window:

```powershell
$shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$([Environment]::GetFolderPath('Startup'))\docker-wsl-proxy.lnk")
$shortcut.TargetPath = 'conhost.exe'
$shortcut.Arguments = "--headless `"$((Get-Command docker-wsl-proxy).Source)`""
$shortcut.Save()
```

To stop it, end `docker-wsl-proxy.exe` in the Task Manager. To undo, delete the shortcut.

## What it maps

In the bind mounts of a container that is created (`-v`, `--mount type=bind`, Compose `volumes`, or the API):

- `C:\dir` and `C:/dir` become `/mnt/c/dir`. The root of the drives is read from the `[automount]` section of `/etc/wsl.conf` in the distribution, and is `/mnt/` by default.
- `\\wsl.localhost\<distro>\dir` and `\\wsl$\<distro>\dir` become `/dir`.
- Paths of WSL, such as `/home/me`, and names of volumes are left alone.

Everything else goes to the daemon unchanged. What a client does with files by itself already works, because the client reads them on Windows: build contexts, `-f`, `--env-file`, `docker cp`, Compose files and their relative paths.

## How it reaches WSL

With nothing set up in WSL, each connection runs `wsl.exe -e docker system dial-stdio`. `wsl.exe` takes about 100 ms to start, so two connections are kept waiting ahead.

Those two keep WSL running for as long as docker-wsl-proxy runs. With Docker in WSL that is usually wanted: WSL stops a distribution when nothing started from Windows is left in it, [even with systemd services running](https://learn.microsoft.com/en-us/windows/wsl/systemd), and the containers stop with it.

### A faster connection

A listener in WSL on the vsock port 2375 needs no `wsl.exe`: docker-wsl-proxy finds it at start, in about 3 ms, and connects through a Hyper-V socket in about 1 ms. To set it up, in the distribution that runs Docker:

```bash
sudo apt-get install -y socat
sudo tee /etc/systemd/system/docker-wsl-proxy.service >/dev/null <<'UNIT'
[Unit]
Description=Docker API on vsock port 2375, for docker-wsl-proxy on Windows
Requires=docker.socket
After=docker.socket

[Service]
# -t: an attach whose input ended still receives the output of the container.
ExecStart=/usr/bin/socat -t 86400 VSOCK-LISTEN:2375,fork UNIX-CONNECT:/run/docker.sock
DynamicUser=yes
SupplementaryGroups=docker
# socat ends with 143 on SIGTERM.
SuccessExitStatus=143

[Install]
WantedBy=multi-user.target
UNIT
sudo systemctl enable --now docker-wsl-proxy.service
```

With the listener:

- Nothing keeps WSL running, so it can stop when idle. The next request then waits a second, goes through `wsl.exe`, which starts WSL again, and the listener is found again within 30 seconds.
- The Docker socket, which is root in WSL, is open to whoever can open a Hyper-V socket to the virtual machine of WSL: the programs of your Windows user. Users in WSL outside the `docker` group cannot reach it, as vsock has no loopback in WSL. Whether other Windows users or administrators can was not tested.

On the machine it was measured on, the gain is small, because the two waiting connections already hide the start of `wsl.exe`: `docker version` took 70 to 85 ms without the listener and 68 to 75 ms with it.

## Limits

- It cannot run next to Docker Desktop, or next to another docker-wsl-proxy: it refuses to start when the pipe exists.
- Only your Windows user can open the pipe.
- It gives no `docker` command. Install a client for Windows. docker-wsl-shim is a `docker` command too, but it talks to WSL by itself and does not use the pipe.
- `\\wsl.localhost\<distro>\…` is taken for the distribution that runs Docker, whatever `<distro>` says.
- Paths of network shares, and `device=C:\…` in the options of a volume, are left as they are.
- All WSL 2 distributions share one virtual machine, so a listener is used whichever distribution runs it. `--distro` chooses the distribution for `wsl.exe` and for `wsl.conf` only.
- When the distribution does not exist, or Docker does not run in it, docker-wsl-proxy starts anyway, and each request fails with the message of `wsl.exe` or of `docker` in WSL.
- A shell in `docker run -it` can drop what is typed in the same few milliseconds as its first resize, which can come right after the prompt, through `wsl.exe`.
- Linux containers only: it is a way to the daemon in WSL.
- Tested on Windows 11 on `amd64`. The `arm64` build is not tested.

## Why is it made this way?

- **Docker does not map the paths of the client.** A bind mount is [resolved on the host of the daemon](https://docs.docker.com/engine/storage/bind-mounts/), and the same page says that Docker Desktop has its own mechanisms for it. A request for mapping rules in the docker cli, [docker/cli#3786](https://github.com/docker/cli/issues/3786), written for this very case, is open since 2022 with no answer. Compose can [convert Windows paths](https://docs.docker.com/compose/how-tos/environment-variables/envvars/) by itself, but to `/c/dir`, which is not where WSL has the drives.
- **It serves the pipe that Docker Desktop serves**, so that no client needs a setting. The price is that the two cannot run together.
- **A named pipe, and no TCP port.** The usual advice for Docker in WSL is to make the daemon listen on `tcp://localhost:2375`, which Docker [warns against](https://docs.docker.com/engine/daemon/remote-access/): whoever reaches the port is root in WSL. The pipe is open to your user only, and nothing in WSL has to change.
- **Only a daemon, with no wrapper for `docker.exe`.** A wrapper that started a proxy for each command was built first, in [docker-on-docker-shim#33](https://github.com/felipecrs/docker-on-docker-shim/pull/33), and dropped: it was slower than the plain cli with the daemon running, it started `wsl.exe` twice for every command, and it covered only programs started through it. For a `docker` command with nothing running there is docker-wsl-shim ([docker-on-docker-shim#31](https://github.com/felipecrs/docker-on-docker-shim/issues/31)).
- **`wsl.exe` first, the listener as an option.** `wsl.exe` works with nothing installed in WSL. The listener is faster by a little and needs root in WSL once.
- **`socat` for the listener, and not `systemd-socket-proxyd`**, which loses the output of an attach without input, such as `docker run` without `-i`.
- **It looks for the virtual machine of WSL by trying.** A Hyper-V socket needs the ID of the virtual machine, which changes at each start of WSL. docker-wsl-proxy reads the running ones from the registry and tries the port in each, for 50 ms at most at start.
- **Options on the command line, with no environment variables and no config file.** It is started in one place, and the options are in that place.
- **The proxy itself is not here.** It is the Go package [`proxy`](https://github.com/felipecrs/docker-on-docker-shim/tree/master/proxy) of [docker-on-docker-shim](https://github.com/felipecrs/docker-on-docker-shim), which uses it to map the paths of a container to its host. This project adds the paths of Windows, the way to WSL and the pipe. They are two projects because their users have nothing in common ([docker-on-docker-shim#35](https://github.com/felipecrs/docker-on-docker-shim/issues/35)).

## Alternatives

- **Docker Desktop**: see [above](#is-it-for-you).
- **[Rancher Desktop](https://github.com/rancher-sandbox/rancher-desktop) and [Podman Desktop](https://github.com/podman-desktop/podman-desktop)** are free and open source. They bring an engine of their own, in a WSL distribution of their own, in place of the Docker you installed in yours.
- **`tcp://localhost:2375`** gives Windows an endpoint, with the risk described above, and maps no paths.
- **A [docker context over SSH](https://docs.docker.com/engine/security/protect-access/)** needs an SSH server in WSL, and maps no paths.

## Development

See [AGENTS.md](AGENTS.md).

## See also

- [docker-wsl-shim](https://github.com/felipecrs/docker-wsl-shim), a `docker.exe` that runs the docker cli of WSL.
- [docker-on-docker-shim](https://github.com/felipecrs/docker-on-docker-shim), which makes bind mounts work in a container that uses the Docker of its host.
