#!/bin/sh
# install.sh - download and install the hrisa daemon and the hrisactl CLI.
#
# Usage:
#   sudo ./install.sh                 # install daemon + CLI + systemd unit
#   sudo ./install.sh --cli-only      # install only the hrisactl client
#   ./install.sh --prefix "$HOME/.local" --no-service   # user-local, no root
#
# Environment overrides:
#   PREFIX     install prefix              (default: /usr/local)
#   BINDIR     binary dir                  (default: $PREFIX/bin)
#   UNITDIR    systemd unit dir            (default: /etc/systemd/system)
#   VERSION    version tag to fetch        (default: latest)

set -eu

# ---------------------------------------------------------------------------
# Download URLs -- REPLACE these placeholders with your real release URLs.
#
# Each URL is a template: the tokens {version} and {arch} are substituted at
# runtime by expand_url() below. {version} comes from --version/$VERSION
# (default "latest"); {arch} is "amd64" or "arm64", detected from `uname -m`.
# So you publish one file per architecture at each version and this script
# picks the right one for the machine it runs on.
# ---------------------------------------------------------------------------

# HRISA_URL: the daemon (main program) binary -- the long-running server that
# listens on the local Unix socket. Downloaded and installed to $BINDIR/hrisa
# unless --cli-only is given.
HRISA_URL="https://REPLACE-ME.example.com/hrisa/{version}/hrisa-linux-{arch}"

# HRISACTL_URL: the CLI client binary -- the `hrisactl` command users run to
# talk to the daemon over the socket. Always downloaded and installed to
# $BINDIR/hrisactl (this is the only file fetched in --cli-only mode).
HRISACTL_URL="https://REPLACE-ME.example.com/hrisa/{version}/hrisactl-linux-{arch}"

# Note: the systemd unit is NOT downloaded. It is static text (no build output
# in it), so write_service_file() below generates it locally instead.

# write_service_file <dest> -- write the systemd unit for the hrisa daemon.
# Kept in sync with packaging/hrisa.service in the repo.
write_service_file() {
	cat >"$1" <<'EOF'
[Unit]
Description=hrisa daemon (local-socket command server)
Documentation=man:hrisa(8)
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/hrisa
Restart=on-failure
RestartSec=2

Group=hrisa

RuntimeDirectory=hrisa
RuntimeDirectoryMode=0750
Environment=HRISA_SOCKET=/run/hrisa/hrisa.sock

NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX
RestrictNamespaces=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes

[Install]
WantedBy=multi-user.target
EOF
}

# ---------------------------------------------------------------------------
# Defaults / options
# ---------------------------------------------------------------------------
PREFIX="${PREFIX:-/usr/local}"
UNITDIR="${UNITDIR:-/etc/systemd/system}"
VERSION="${VERSION:-latest}"
CLI_ONLY=0
INSTALL_SERVICE=1
ENABLE_SERVICE=1

while [ $# -gt 0 ]; do
	case "$1" in
		--cli-only)     CLI_ONLY=1 ;;
		--no-service)   INSTALL_SERVICE=0; ENABLE_SERVICE=0 ;;
		--no-enable)    ENABLE_SERVICE=0 ;;
		--prefix)       PREFIX="$2"; shift ;;
		--version)      VERSION="$2"; shift ;;
		-h|--help)
			sed -n '2,20p' "$0"
			exit 0 ;;
		*)
			echo "install.sh: unknown option '$1'" >&2
			exit 2 ;;
	esac
	shift
done

BINDIR="${BINDIR:-$PREFIX/bin}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo "==> $*"; }
die()  { echo "install.sh: $*" >&2; exit 1; }

need() {
	command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

# Detect a fetch tool (curl or wget) into $FETCH.
detect_fetch() {
	if command -v curl >/dev/null 2>&1; then
		FETCH="curl -fSL -o"
	elif command -v wget >/dev/null 2>&1; then
		FETCH="wget -O"
	else
		die "need curl or wget to download files"
	fi
}

# Map uname -m to a release arch string.
detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64)   ARCH="amd64" ;;
		aarch64|arm64)  ARCH="arm64" ;;
		*)              die "unsupported architecture: $(uname -m)" ;;
	esac
}

# expand_url <template> -> substitutes {version} and {arch}.
expand_url() {
	echo "$1" | sed -e "s|{version}|$VERSION|g" -e "s|{arch}|$ARCH|g"
}

# fetch <url> <dest>
fetch() {
	log "downloading $1"
	# shellcheck disable=SC2086
	$FETCH "$2" "$1" || die "download failed: $1"
}

# install_file <src> <dest> <mode>  (uses sudo automatically if needed)
install_file() {
	src="$1"; dest="$2"; mode="$3"
	destdir="$(dirname "$dest")"
	$SUDO install -d "$destdir"
	$SUDO install -m "$mode" "$src" "$dest"
	log "installed $dest"
}

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
[ "$(uname -s)" = "Linux" ] || die "this installer supports Linux only"
need install
need sed
detect_fetch
detect_arch

# Decide whether we need sudo for the chosen destinations.
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
	if [ ! -w "$(dirname "$BINDIR")" ] 2>/dev/null || [ "$INSTALL_SERVICE" -eq 1 ]; then
		if command -v sudo >/dev/null 2>&1; then
			SUDO="sudo"
		fi
	fi
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

log "hrisa installer  (version=$VERSION arch=$ARCH prefix=$PREFIX)"

# ---------------------------------------------------------------------------
# Download + install the CLI (always) and the daemon (unless --cli-only)
# ---------------------------------------------------------------------------
fetch "$(expand_url "$HRISACTL_URL")" "$TMP/hrisactl"
install_file "$TMP/hrisactl" "$BINDIR/hrisactl" 0755

if [ "$CLI_ONLY" -eq 0 ]; then
	fetch "$(expand_url "$HRISA_URL")" "$TMP/hrisa"
	install_file "$TMP/hrisa" "$BINDIR/hrisa" 0755

	if [ "$INSTALL_SERVICE" -eq 1 ] && command -v systemctl >/dev/null 2>&1; then
		log "generating systemd unit"
		write_service_file "$TMP/hrisa.service"
		install_file "$TMP/hrisa.service" "$UNITDIR/hrisa.service" 0644

		# Create the "hrisa" group and add the invoking user so hrisactl works
		# without sudo (the daemon's socket is owned root:hrisa, mode 0660).
		if command -v groupadd >/dev/null 2>&1; then
			$SUDO groupadd -f hrisa
			if [ -n "${SUDO_USER:-}" ]; then
				$SUDO usermod -aG hrisa "$SUDO_USER" && \
					log "added $SUDO_USER to group 'hrisa' — log out and back in for it to take effect"
			else
				log "add your user to the 'hrisa' group: sudo usermod -aG hrisa <you>  (then re-login)"
			fi
		fi
		$SUDO systemctl daemon-reload
		if [ "$ENABLE_SERVICE" -eq 1 ]; then
			$SUDO systemctl enable --now hrisa.service
			log "hrisa.service enabled and started"
		else
			log "installed unit; start it with: sudo systemctl enable --now hrisa.service"
		fi
	elif [ "$INSTALL_SERVICE" -eq 1 ]; then
		log "systemctl not found; skipping service install"
		log "run the daemon manually with: $BINDIR/hrisa"
	fi
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
log "done."
case ":$PATH:" in
	*":$BINDIR:"*) : ;;
	*) log "note: $BINDIR is not on your PATH; add it to use hrisactl directly" ;;
esac
log "try:  hrisactl status"
