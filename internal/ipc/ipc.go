// Package ipc defines the local-socket protocol shared between the hrisa
// daemon (the "main program") and the hrisactl command-line client.
//
// The transport is a Unix domain socket. Messages are newline-delimited JSON:
// the client writes one Request per line and the server replies with exactly
// one Response line per request. This keeps the wire format trivial to debug
// (e.g. with `socat`/`nc`) while remaining structured.
package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DefaultSocketPath is where the daemon listens unless overridden.
const DefaultSocketPath = "/run/hrisa/hrisa.sock"

// FallbackSocketPath is used when DefaultSocketPath's directory is not
// writable (e.g. running as a non-root user without the /run/hrisa dir).
const FallbackSocketPath = "/tmp/hrisa.sock"

// SocketEnv is the environment variable that overrides the socket path for
// both the daemon and the client.
const SocketEnv = "HRISA_SOCKET"

// Request is a single command sent by the client to the daemon.
type Request struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	IP      string   `json:"ip,omitempty"`
	Mode    string   `json:"mode,omitempty"`
}

// Response is the daemon's reply to a Request.
type Response struct {
	OK    bool   `json:"ok"`
	Data  string `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// SocketPath is a convenience alias kept for callers that don't care whether
// they are the server or the client. Prefer ServerSocketPath / ClientSocketPath.
func SocketPath() string { return ServerSocketPath() }

// ServerSocketPath resolves where the daemon should CREATE its socket:
// HRISA_SOCKET if set, else the canonical /run/hrisa path when that directory
// is writable (i.e. running as root/systemd), else the /tmp fallback.
func ServerSocketPath() string {
	if p := os.Getenv(SocketEnv); p != "" {
		return p
	}
	dir := filepath.Dir(DefaultSocketPath)
	if err := os.MkdirAll(dir, 0o755); err == nil && writable(dir) {
		return DefaultSocketPath
	}
	return FallbackSocketPath
}

// ClientSocketPath resolves which socket the CLI should CONNECT to. Unlike the
// server it must not use writability to decide (the client never creates the
// socket, and /run/hrisa is typically not writable by a normal user even when
// the daemon is listening there). It prefers HRISA_SOCKET, then whichever of
// the canonical/fallback paths actually exists, defaulting to the canonical
// path so error messages point at the expected location.
func ClientSocketPath() string {
	if p := os.Getenv(SocketEnv); p != "" {
		return p
	}
	if exists(DefaultSocketPath) {
		return DefaultSocketPath
	}
	if exists(FallbackSocketPath) {
		return FallbackSocketPath
	}
	return DefaultSocketPath
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".hrisa-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// WriteRequest encodes and writes a single newline-delimited request.
func WriteRequest(w io.Writer, req Request) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// WriteResponse encodes and writes a single newline-delimited response.
func WriteResponse(w io.Writer, resp Response) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// ReadRequest reads one newline-delimited request from r.
func ReadRequest(r *bufio.Reader) (Request, error) {
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Request{}, err
	}
	var req Request
	if uerr := json.Unmarshal(line, &req); uerr != nil {
		return Request{}, fmt.Errorf("malformed request: %w", uerr)
	}
	return req, nil
}

// ReadResponse reads one newline-delimited response from r.
func ReadResponse(r *bufio.Reader) (Response, error) {
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Response{}, err
	}
	var resp Response
	if uerr := json.Unmarshal(line, &resp); uerr != nil {
		return Response{}, fmt.Errorf("malformed response: %w", uerr)
	}
	return resp, nil
}
