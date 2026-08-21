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

// ANSI color codes and a global switch. Colors are enabled only when writing
// to a terminal and NO_COLOR is unset (see initColor).
const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
	cGray   = "\033[90m"
)

var useColor bool

// col wraps s in the given ANSI code(s) when color is enabled.
func col(code, s string) string {
	if !useColor {
		return s
	}
	return code + s + cReset
}

// initColor decides whether to emit ANSI codes: honor an explicit --no-color /
// NO_COLOR, otherwise enable only when stdout is a terminal.
func initColor(disabled bool) {
	if disabled || os.Getenv("NO_COLOR") != "" {
		useColor = false
		return
	}
	fi, err := os.Stdout.Stat()
	useColor = err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func main() {
	socket := flag.String("socket", ipc.ClientSocketPath(), "path to the daemon's Unix domain socket")
	timeout := flag.Duration("timeout", 5*time.Second, "per-command response timeout")
	noColor := flag.Bool("no-color", false, "disable colored output")
	flag.Usage = usage
	flag.Parse()

	initColor(*noColor)

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
	fmt.Printf("%s %s - connected to %s\n",
		col(cBold+cGreen, "hrisactl"), col(cBold, version), col(cCyan, socket))
	fmt.Println(col(cGray, "type 'help' for commands, \"use <string>\" to set context, 'quit' to exit"))

	// context holds the string set via `use '<string>'`. When non-empty it is
	// shown in the prompt between parentheses, e.g. "(target-1) > ".
	context := ""

	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(prompt(context))
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

		fields := strings.Fields(line)
		cmd := strings.ToLower(fields[0])

		// Client-side commands, handled without touching the daemon.
		switch cmd {
		case "quit", "exit":
			return
		case "clear":
			fmt.Print("\033[2J\033[H")
			continue
		case "use":
			// Save everything after "use" (quotes stripped) as the context.
			context = unquote(strings.TrimSpace(strings.TrimPrefix(line, fields[0])))
			if context == "" {
				fmt.Println(col(cYellow, "context cleared"))
			} else {
				fmt.Printf("using %s\n", col(cBold+cCyan, context))
			}
			continue
		}

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

// prompt renders the input prompt, including the saved context when set.
func prompt(context string) string {
	arrow := col(cBold+cGreen, ">")
	if context != "" {
		return fmt.Sprintf("%s%s%s %s ",
			col(cGray, "("), col(cBold+cCyan, context), col(cGray, ")"), arrow)
	}
	return arrow + " "
}

// unquote strips a single pair of surrounding single or double quotes, so
// `use 'my target'` saves `my target` rather than `'my target'`.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
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
		fmt.Fprintln(os.Stderr, col(cRed, "error: "+resp.Error))
		return nil
	}
	if resp.Data != "" {
		fmt.Println(col(cGreen, resp.Data))
	}
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `hrisactl - interactive client for the hrisa daemon

usage:
  hrisactl [flags]

starts an interactive prompt (>). type daemon commands like 'status', 'ping',
'stats', 'version', 'echo ...', 'shutdown', or 'help'. client-side commands:
"use '<string>'" (save a context shown as "(<string>) > "; run 'use' with no
argument to clear it), 'clear', and 'quit'/'exit' (or Ctrl-D) to leave.

flags:
`)
	flag.PrintDefaults()
}
