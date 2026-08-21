// Command hrisactl is the interactive CLI app for the hrisa daemon. It opens a
// connection to the daemon's local Unix domain socket and drops you into a
// prompt where you type commands:
//
//	$ hrisactl
//	hrisactl 0.1.0 - connected to /run/hrisa/hrisa.sock
//	type 'help' for commands, 'quit' to exit
//	> status
//	...
//	> quit
//
// Flags select the socket and timeout:
//
//	hrisactl -socket /tmp/hrisa.sock
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"hrisa.com/internal/ipc"
)

const version = "0.1.0"

func main() {
	socket := flag.String("socket", ipc.ClientSocketPath(), "path to the daemon's Unix domain socket")
	timeout := flag.Duration("timeout", 5*time.Second, "per-command response timeout")
	flag.Usage = usage
	flag.Parse()

	conn, err := net.DialTimeout("unix", *socket, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hrisactl: cannot connect to daemon at %s: %v\n", *socket, err)
		fmt.Fprintln(os.Stderr, "hrisactl: is the hrisa daemon running? (set HRISA_SOCKET or -socket if it uses a different path)")
		os.Exit(1)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	repl(conn, r, *socket, *timeout)
}

// repl runs the interactive prompt against an open connection.
func repl(conn net.Conn, r *bufio.Reader, socket string, timeout time.Duration) {
	fmt.Printf("hrisactl %s - connected to %s\n", version, socket)
	fmt.Println("type 'help' for commands, 'quit' to exit")

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !in.Scan() {
			// EOF (Ctrl-D) or a read error: exit. Report a genuine error;
			// a plain EOF (in.Err() == nil) is a clean exit.
			fmt.Println()
			if err := in.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "hrisactl: input error: %v\n", err)
			}
			return
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}

		// Client-side commands, handled without touching the daemon.
		switch strings.ToLower(line) {
		case "quit", "exit":
			return
		case "clear":
			fmt.Print("\033[2J\033[H")
			continue
		}

		fields := strings.Fields(line)
		req := ipc.Request{Command: fields[0], Args: fields[1:]}

		conn.SetDeadline(time.Now().Add(timeout))
		if err := send(conn, r, req); err != nil {
			// A broken connection means the daemon went away; nothing left to do.
			fmt.Fprintf(os.Stderr, "hrisactl: connection lost: %v\n", err)
			return
		}

		// After a successful shutdown the daemon closes the socket; leave.
		if strings.EqualFold(req.Command, "shutdown") {
			return
		}
	}
}

// send writes one request and prints the daemon's response. It returns a
// non-nil error only when the connection itself is unusable.
func send(conn net.Conn, r *bufio.Reader, req ipc.Request) error {
	if err := ipc.WriteRequest(conn, req); err != nil {
		return err
	}
	resp, err := ipc.ReadResponse(r)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("daemon closed the connection")
		}
		return err
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "error: %s\n", resp.Error)
		return nil
	}
	if resp.Data != "" {
		fmt.Println(resp.Data)
	}
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `hrisactl - interactive client for the hrisa daemon

usage:
  hrisactl [flags]

starts an interactive prompt (>). type daemon commands like 'status', 'ping',
'stats', 'version', 'echo ...', 'shutdown', or 'help'. client-side commands:
'clear', and 'quit'/'exit' (or Ctrl-D) to leave.

flags:
`)
	flag.PrintDefaults()
}
