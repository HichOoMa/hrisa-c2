package main

import (
	"flag"
	"io"
	"log"
	"net"
)

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
	log.Printf("connection from %s", conn.RemoteAddr())

	// Echo whatever the client sends back to it.
	if _, err := io.Copy(conn, conn); err != nil {
		log.Printf("connection %s closed: %v", conn.RemoteAddr(), err)
	}
}
