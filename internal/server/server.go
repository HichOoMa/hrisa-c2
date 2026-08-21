// Package server implements the hrisa daemon: it listens on a local Unix
// domain socket and answers commands sent by the hrisactl CLI client.
package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"hrisa.com/internal/ipc"
)

// Server owns the listening socket and per-connection state.
type Server struct {
	socketPath  string
	connections map[string]net.Conn
	listener    net.Listener
	startedAt   time.Time

	// active tracks the number of currently open client connections.
	active int64
	// total tracks the number of connections handled since start.
	total int64

	shutdown chan struct{}
}

// New creates a Server bound to socketPath (the socket is not opened yet).
func New(socketPath string, connections map[string]net.Conn) *Server {
	return &Server{
		socketPath:  socketPath,
		connections: connections,
		shutdown:    make(chan struct{}),
	}
}

// Listen opens the Unix socket, removing any stale socket file first.
func (s *Server) Listen() error {
	// A leftover socket file from a previous crash would make Listen fail
	// with "address already in use"; clear it if nothing is listening.
	if _, err := os.Stat(s.socketPath); err == nil {
		if c, derr := net.Dial("unix", s.socketPath); derr == nil {
			c.Close()
			return fmt.Errorf("another hrisa daemon is already listening on %s", s.socketPath)
		}
		if rerr := os.Remove(s.socketPath); rerr != nil {
			return fmt.Errorf("removing stale socket %s: %w", s.socketPath, rerr)
		}
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.socketPath, err)
	}
	// Allow non-root clients on the same host to connect.
	if err := os.Chmod(s.socketPath, 0o660); err != nil {
		log.Printf("warning: chmod %s: %v", s.socketPath, err)
	}
	s.listener = ln
	s.startedAt = time.Now()
	return nil
}

// Serve accepts connections until the context is cancelled or Stop is called.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		select {
		case <-ctx.Done():
		case <-s.shutdown:
		}
		s.listener.Close()
	}()

	log.Printf("hrisa daemon listening on %s (pid %d)", s.socketPath, os.Getpid())
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Accept fails once the listener is closed during shutdown.
			select {
			case <-ctx.Done():
				return nil
			case <-s.shutdown:
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go s.handleConn(conn)
	}
}

// Stop signals Serve to stop accepting and removes the socket file.
func (s *Server) Stop() {
	select {
	case <-s.shutdown:
		// already closed
	default:
		close(s.shutdown)
	}
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.socketPath)
}

func (s *Server) handleConn(conn net.Conn) {
	atomic.AddInt64(&s.active, 1)
	atomic.AddInt64(&s.total, 1)
	defer func() {
		atomic.AddInt64(&s.active, -1)
		conn.Close()
	}()

	r := bufio.NewReader(conn)
	for {
		req, err := ipc.ReadRequest(r)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("read error: %v", err)
			}
			return
		}
		resp := s.dispatch(req)
		if werr := ipc.WriteResponse(conn, resp); werr != nil {
			log.Printf("write error: %v", werr)
			return
		}
		if req.Command == "shutdown" && resp.OK {
			// Reply first, then trigger shutdown so the client sees the ack.
			go s.Stop()
			return
		}
	}
}

// dispatch maps a request to a response. Add new commands here.
//
// req.Context carries the client's active `use <string>` context. Commands
// behave differently when it is set, so the same command run under a context
// is not the same as run bare.
func (s *Server) dispatch(req ipc.Request) ipc.Response {
	switch strings.ToLower(strings.TrimSpace(req.IP)) {
	case "":
		switch strings.ToLower(strings.TrimSpace(req.Command)) {
		case "ls":
			return ipc.Response{OK: true, Data: s.ls()}
		case "ping":
			return ipc.Response{OK: true, Data: "pong"}

		case "version":
			return ipc.Response{OK: true, Data: Version}

		case "status":
			uptime := time.Since(s.startedAt).Round(time.Second)
			data := fmt.Sprintf(
				"pid:        %d\nsocket:     %s\nuptime:     %s\nactive:     %d\nhandled:    %d\nversion:    %s",
				os.Getpid(), s.socketPath, uptime,
				atomic.LoadInt64(&s.active), atomic.LoadInt64(&s.total), Version,
			)
			return ipc.Response{OK: true, Data: data}

		case "stats":
			data := fmt.Sprintf("active=%d handled=%d",
				atomic.LoadInt64(&s.active), atomic.LoadInt64(&s.total))
			return ipc.Response{OK: true, Data: data}

		case "echo":
			msg := strings.Join(req.Args, " ")
			return ipc.Response{OK: true, Data: msg}

		case "shutdown":
			return ipc.Response{OK: true, Data: "shutting down"}

		case "help":
			return ipc.Response{OK: true, Data: helpText}

		case "":
			return ipc.Response{OK: false, Error: "empty command"}

		default:
			return ipc.Response{OK: false, Error: fmt.Sprintf("unknown command %q (try 'help')", req.Command)}
		}
	default:
		if net.ParseIP(req.IP) == nil {
			return ipc.Response{OK: false, Error: fmt.Sprintf("invalid IP address %q", req.IP)}
		} else {
			// TODO implement context-specific commands here. For now, just reject any command under a context.
			return ipc.Response{OK: false, Error: fmt.Sprintf("unknown command %q under context %q (try 'help')", req.Command, req.IP)}
		}
	}
}

func (s *Server) ls() string {
	connections := make([]string, 0, len(s.connections))
	for ip := range s.connections {
		connections = append(connections, ip)
	}
	return strings.Join(connections, "\n")
}

// Version is the daemon/protocol version, overridable at build time via
// -ldflags "-X hrisa.com/internal/server.Version=...".
var Version = "0.1.0"

const helpText = `available commands:
  ping              health check, replies "pong"
  status            daemon pid, uptime, connection counters
  stats             machine-readable connection counters
  version           daemon version
  echo <args...>    echo the arguments back
  shutdown          ask the daemon to exit gracefully
  help              this message`
