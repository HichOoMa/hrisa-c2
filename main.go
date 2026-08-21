// Command hrisa is the main program: a long-running daemon that listens on a
// local Unix domain socket (for hrisactl) and a TCP echo port, serving both
// until terminated.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
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

	port := flag.String("port", "8080", "TCP port to listen on")
	socket := flag.String("socket", ipc.ServerSocketPath(), "path to the Unix domain socket to listen on")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("hrisa: ")

	// One shared context/signal handler for both servers: since
	// signal.Notify intercepts SIGINT/SIGTERM, Go's default "terminate on
	// signal" behavior no longer applies, so every listener MUST watch the
	// same cancellation or the process becomes unkillable (whichever server
	// doesn't react to the signal keeps main()'s WaitGroup from ever
	// returning).
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sig
		log.Printf("received %s, shutting down", s)
		cancel()
	}()

	// Run both servers concurrently and block here until both exit, so the
	// process stays alive to actually serve requests (a bare `go f()` in
	// main with no wait would let main return and kill the process before
	// either listener does any work).
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := startTcp(ctx, *port); err != nil {
			log.Printf("tcp server error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		startSocket(ctx, *socket)
	}()
	wg.Wait()
	log.Print("stopped")
}

func startTcp(ctx context.Context, port string) error {
	addr := net.JoinHostPort("0.0.0.0", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	defer listener.Close()

	log.Printf("TCP server listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
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

func startSocket(ctx context.Context, socket string) {
	srv := server.New(socket, connections)
	if err := srv.Listen(); err != nil {
		log.Fatalf("startup failed: %v", err)
	}

	go func() {
		<-ctx.Done()
		srv.Stop()
	}()

	if err := srv.Serve(ctx); err != nil {
		log.Printf("serve error: %v", err)
	}
	srv.Stop()
}
