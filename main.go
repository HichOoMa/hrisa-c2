// Command hrisa is the main program: a long-running daemon that listens on a
// local Unix domain socket and serves commands issued by the hrisactl CLI.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"hrisa.com/internal/ipc"
	"hrisa.com/internal/server"
)

var (
	active      atomic.Int64
	connections map[string]net.Conn
)

func main() {
	connections = make(map[string]net.Conn)
	go startTcp()
	go startSocket()
}

func startTcp() error {
	port := flag.String("port", "8080", "TCP port to listen on")
	flag.Parse()

	addr := net.JoinHostPort("0.0.0.0", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", addr, err)
	}
	defer listener.Close()

	log.Printf("TCP server listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()

	n := active.Add(1)
	connections[conn.RemoteAddr().String()] = conn
	log.Printf("connection from %s (active: %d)", conn.RemoteAddr(), n)
	defer func() {
		n := active.Add(-1)
		delete(connections, conn.RemoteAddr().String())
		log.Printf("connection %s closed (active: %d)", conn.RemoteAddr(), n)
	}()

	// Echo whatever the client sends back to it.
	if _, err := io.Copy(conn, conn); err != nil {
		log.Printf("connection %s error: %v", conn.RemoteAddr(), err)
	}
}

func startSocket() {
	socket := flag.String("socket", ipc.ServerSocketPath(), "path to the Unix domain socket to listen on")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("hrisa: ")

	srv := server.New(*socket)
	if err := srv.Listen(); err != nil {
		log.Fatalf("startup failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Translate SIGINT/SIGTERM into a graceful shutdown so the socket file
	// is removed on exit.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sig
		log.Printf("received %s, shutting down", s)
		srv.Stop()
		cancel()
	}()

	if err := srv.Serve(ctx); err != nil {
		log.Fatalf("serve error: %v", err)
	}
	srv.Stop()
	log.Print("stopped")
}
