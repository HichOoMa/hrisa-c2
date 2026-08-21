// Command hrisa is the main program: a long-running daemon that listens on a
// local Unix domain socket and serves commands issued by the hrisactl CLI.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"hrisa.com/internal/ipc"
	"hrisa.com/internal/server"
)

func main() {
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
