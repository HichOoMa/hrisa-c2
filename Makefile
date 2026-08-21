# Makefile for the hrisa daemon and the hrisactl CLI client.

# GO can be overridden if `go` is not on PATH (e.g. under sudo, which strips
# mise/asdf PATH entries):  make GO=$(command -v go) install
GO          ?= go
PREFIX      ?= /usr/local
BINDIR      ?= $(PREFIX)/bin
UNITDIR     ?= /etc/systemd/system
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X hrisa.com/internal/server.Version=$(VERSION)

SRC         := $(shell find . -name '*.go') go.mod
BINS        := bin/hrisa bin/hrisactl

.PHONY: all build daemon cli clean install install-cli uninstall enable

all: build

build: $(BINS)
daemon: bin/hrisa
cli: bin/hrisactl

# File targets: `make install` depends on these by timestamp, so a prior
# `make build` (run as your user) is reused and NOT recompiled under sudo.
bin/hrisa: $(SRC)
	$(GO) build -ldflags "$(LDFLAGS)" -o $@ .

bin/hrisactl: $(SRC)
	$(GO) build -ldflags "$(LDFLAGS)" -o $@ ./cmd/hrisactl

clean:
	rm -rf bin

# Full install: both binaries + systemd unit (needs root).
# Recommended flow:  make build   &&   sudo make install
install: $(BINS)
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 bin/hrisa    $(DESTDIR)$(BINDIR)/hrisa
	install -m 0755 bin/hrisactl $(DESTDIR)$(BINDIR)/hrisactl
	install -d $(DESTDIR)$(UNITDIR)
	install -m 0644 packaging/hrisa.service $(DESTDIR)$(UNITDIR)/hrisa.service
	@# Create the "hrisa" group and add the invoking user so hrisactl works
	@# without sudo. Harmless if the group already exists.
	groupadd -f hrisa
	@if [ -n "$(SUDO_USER)" ]; then \
		usermod -aG hrisa "$(SUDO_USER)" && \
		echo "Added $(SUDO_USER) to group 'hrisa' — log out and back in for it to take effect."; \
	else \
		echo "Add your user to the 'hrisa' group:  sudo usermod -aG hrisa <you>  (then re-login)"; \
	fi
	@echo "Installed. Enable the daemon with: sudo make enable"

# Install just the CLI (no root strictly required if BINDIR is user-writable).
install-cli: bin/hrisactl
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 bin/hrisactl $(DESTDIR)$(BINDIR)/hrisactl

enable:
	systemctl daemon-reload
	systemctl enable --now hrisa.service
	@echo "hrisa daemon started. Try: hrisactl status"

uninstall:
	-systemctl disable --now hrisa.service 2>/dev/null || true
	rm -f $(DESTDIR)$(BINDIR)/hrisa $(DESTDIR)$(BINDIR)/hrisactl
	rm -f $(DESTDIR)$(UNITDIR)/hrisa.service
	systemctl daemon-reload 2>/dev/null || true
