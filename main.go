package main

import (
	"flag"
	"io"
	"log"
	"net"
	"sync/atomic"
)

// active tracks the number of currently open connections.
var active int64

func main() {
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

	n := atomic.AddInt64(&active, 1)
	log.Printf("connection from %s (active: %d)", conn.RemoteAddr(), n)
	defer func() {
		n := atomic.AddInt64(&active, -1)
		log.Printf("connection %s closed (active: %d)", conn.RemoteAddr(), n)
	}()

	// Echo whatever the client sends back to it.
	if _, err := io.Copy(conn, conn); err != nil {
		log.Printf("connection %s error: %v", conn.RemoteAddr(), err)
	}
}
