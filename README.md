# hrisa

A local daemon (`hrisa`) and a Linux CLI client (`hrisactl`) that talk over a
**Unix domain socket**.

## Components

| Binary     | Role                                                             |
|------------|-----------------------------------------------------------------|
| `hrisa`    | Main program / daemon. Listens on a local socket, serves commands. |
| `hrisactl` | CLI client. Connects to the socket and issues commands.         |

Wire protocol lives in [internal/ipc/ipc.go](internal/ipc/ipc.go): newline-delimited
JSON requests/responses. The socket path resolves in this order:

1. `HRISA_SOCKET` environment variable
2. `/run/hrisa/hrisa.sock` (if that directory is writable)
3. `/tmp/hrisa.sock` (fallback)

Both binaries also accept a `-socket <path>` flag.

## Build

```sh
make build        # produces bin/hrisa and bin/hrisactl
```

## Install on Linux

```sh
sudo make install   # installs both binaries to /usr/local/bin + systemd unit
sudo make enable    # daemon-reload, enable + start hrisa.service
```

Install only the CLI (e.g. on a machine that just talks to a remote-managed socket):

```sh
make install-cli PREFIX=$HOME/.local   # installs bin/hrisactl to ~/.local/bin
```

Uninstall:

```sh
sudo make uninstall
```

## Usage

```sh
hrisactl ping                 # -> pong
hrisactl status               # pid, uptime, connection counters
hrisactl stats                # machine-readable counters
hrisactl version
hrisactl echo hello world
hrisactl shutdown             # ask the daemon to exit gracefully
hrisactl help                 # list daemon commands
hrisactl                      # no args -> interactive REPL
```

Point the client at a non-default socket:

```sh
hrisactl -socket /tmp/hrisa.sock status
# or
HRISA_SOCKET=/tmp/hrisa.sock hrisactl status
```

## Run without systemd (dev)

```sh
HRISA_SOCKET=/tmp/hrisa.sock ./bin/hrisa &
HRISA_SOCKET=/tmp/hrisa.sock ./bin/hrisactl status
```

## Adding a command

Add a `case` to `dispatch` in [internal/server/server.go](internal/server/server.go)
and document it in the `help` text. The CLI forwards any command verbatim, so no
client change is needed.
