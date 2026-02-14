package coop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

// AttachOptions configures the interactive attach session.
type AttachOptions struct {
	// DetachKey is the key sequence to detach (default: Ctrl+] = 0x1d).
	DetachKey byte

	// Stdin/Stdout/Stderr for the terminal session. Defaults to os.Stdin/Stdout/Stderr.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// DefaultAttachOptions returns sensible defaults for interactive attach.
func DefaultAttachOptions() AttachOptions {
	return AttachOptions{
		DetachKey: 0x1d, // Ctrl+]
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
	}
}

// Attach connects to the Coop sidecar's WebSocket endpoint in raw terminal mode
// and proxies I/O bidirectionally. It puts the local terminal into raw mode and
// restores it on exit. Detach with Ctrl+] (or configured DetachKey).
//
// Returns nil on clean detach or remote close, error on connection failure.
func (c *Client) Attach(ctx context.Context, opts AttachOptions) error {
	if opts.DetachKey == 0 {
		opts.DetachKey = 0x1d
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	// Build WebSocket URL
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("coop attach: parse url: %w", err)
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	u.Scheme = scheme
	u.Path = strings.TrimRight(u.Path, "/") + "/ws"
	q := u.Query()
	q.Set("mode", "raw")
	if c.token != "" {
		q.Set("token", c.token)
	}
	u.RawQuery = q.Encode()

	// Connect
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("coop attach: ws dial: %w", err)
	}
	defer conn.Close()

	// Put local terminal into raw mode if stdin is a TTY
	stdinFd := -1
	if f, ok := opts.Stdin.(*os.File); ok {
		fd := int(f.Fd())
		if term.IsTerminal(fd) {
			stdinFd = fd
			oldState, err := term.MakeRaw(fd)
			if err != nil {
				return fmt.Errorf("coop attach: raw mode: %w", err)
			}
			defer term.Restore(fd, oldState)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Close the WebSocket when the context is canceled to unblock ReadMessage.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	var wg sync.WaitGroup
	var readErr, writeErr error

	// Handle SIGWINCH for terminal resize
	if stdinFd >= 0 {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGWINCH)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					signal.Stop(sigCh)
					return
				case <-sigCh:
					w, h, err := term.GetSize(stdinFd)
					if err != nil {
						continue
					}
					msg, _ := json.Marshal(map[string]interface{}{
						"type": "resize",
						"cols": w,
						"rows": h,
					})
					conn.WriteMessage(websocket.TextMessage, msg)
				}
			}
		}()

		// Send initial size
		if w, h, err := term.GetSize(stdinFd); err == nil {
			msg, _ := json.Marshal(map[string]interface{}{
				"type": "resize",
				"cols": w,
				"rows": h,
			})
			conn.WriteMessage(websocket.TextMessage, msg)
		}
	}

	// Read from WebSocket → stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() == nil && !websocket.IsCloseError(err,
					websocket.CloseNormalClosure,
					websocket.CloseGoingAway) {
					readErr = fmt.Errorf("coop attach: ws read: %w", err)
				}
				return
			}
			switch msgType {
			case websocket.TextMessage:
				// Could be a JSON control message or raw text
				var envelope struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(data, &envelope) == nil && envelope.Type != "" {
					// Control message — handle exit
					if envelope.Type == WSTypeExit {
						return
					}
					// Output messages contain data field
					if envelope.Type == WSTypeOutput {
						var outMsg struct {
							Data string `json:"data"`
						}
						if json.Unmarshal(data, &outMsg) == nil {
							opts.Stdout.Write([]byte(outMsg.Data))
						}
						continue
					}
					// Screen messages contain lines
					if envelope.Type == WSTypeScreen {
						var scrMsg ScreenResponse
						if json.Unmarshal(data, &scrMsg) == nil {
							for _, line := range scrMsg.Lines {
								opts.Stdout.Write([]byte(line + "\n"))
							}
						}
						continue
					}
					continue
				}
				// Plain text
				opts.Stdout.Write(data)
			case websocket.BinaryMessage:
				opts.Stdout.Write(data)
			}
		}
	}()

	// Read from stdin → WebSocket.
	// Stdin reads can block indefinitely (e.g. io.Pipe, TTY with no input), so we
	// run the blocking read in a separate goroutine and select on both the read
	// result and context cancellation.
	type readResult struct {
		data []byte
		err  error
	}
	stdinCh := make(chan readResult, 1)

	// Background stdin reader — reads continuously and sends to channel.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := opts.Stdin.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				stdinCh <- readResult{data: cp}
			}
			if err != nil {
				stdinCh <- readResult{err: err}
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case res := <-stdinCh:
				if res.err != nil {
					if res.err != io.EOF && ctx.Err() == nil {
						writeErr = fmt.Errorf("coop attach: stdin read: %w", res.err)
					}
					return
				}
				data := res.data

				// Check for detach key
				for i := 0; i < len(data); i++ {
					if data[i] == opts.DetachKey {
						// Send everything before the detach key
						if i > 0 {
							msg, _ := json.Marshal(map[string]string{
								"type": "input",
								"data": string(data[:i]),
							})
							conn.WriteMessage(websocket.TextMessage, msg)
						}
						fmt.Fprintf(opts.Stderr, "\r\nDetached.\r\n")
						return
					}
				}

				msg, _ := json.Marshal(map[string]string{
					"type": "input",
					"data": string(data),
				})
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					if ctx.Err() == nil {
						writeErr = fmt.Errorf("coop attach: ws write: %w", err)
					}
					return
				}
			}
		}
	}()

	wg.Wait()

	if readErr != nil {
		return readErr
	}
	return writeErr
}
