// Package acp implements a minimal Agent Client Protocol (ACP) client that
// drives `goose acp` over stdio (JSON-RPC 2.0). This is the M0 spike/fallback:
// the UI layer can later swap to a richer ACP SDK by replacing only this package.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// OnNotify receives notifications (e.g. session/update).
type OnNotify func(method string, params json.RawMessage)

// OnRequest handles server->client requests (e.g. session/request_permission).
// A nil handler makes the client reply {"code":-32601,"message":"not supported"}.
type OnRequest func(id int, method string, params json.RawMessage) (any, error)

type response struct {
	result json.RawMessage
	err    error
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Client is a minimal ACP JSON-RPC client over a goose subprocess.
type Client struct {
	mu        sync.Mutex
	next      int
	pend      map[int]chan response
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	onNotify  OnNotify
	onRequest OnRequest
	closed    bool
}

// New spawns `gooseBin acp` on stdio and starts the reader loop.
func New(ctx context.Context, gooseBin string, onNotify OnNotify, onRequest OnRequest) (*Client, error) {
	cmd := exec.CommandContext(ctx, gooseBin, "acp")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s acp: %w", gooseBin, err)
	}
	c := &Client{pend: map[int]chan response{}, cmd: cmd, stdin: stdin, onNotify: onNotify, onRequest: onRequest}
	go c.readLoop(stdout)
	return c, nil
}

func (c *Client) write(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("acp: client closed")
	}
	_, err := c.stdin.Write(append(b, '\n'))
	return err
}

// Request sends a JSON-RPC request and waits for the matching response.
func (c *Client) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.next
	c.next++
	ch := make(chan response, 1)
	c.pend[id] = ch
	c.mu.Unlock()

	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if err := c.write(b); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pend, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Notify sends a JSON-RPC notification (no id, no response).
func (c *Client) Notify(method string, params any) error {
	msg := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.write(b)
}

// Close terminates the goose subprocess.
func (c *Client) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
}

func (c *Client) readLoop(r io.Reader) {
	dec := json.NewDecoder(bufio.NewReader(r))
	for {
		var msg map[string]json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			return
		}
		if _, hasMethod := msg["method"]; hasMethod {
			idRaw, hasID := msg["id"]
			if !hasID || string(idRaw) == "null" {
				var method string
				_ = json.Unmarshal(msg["method"], &method)
				if c.onNotify != nil {
					c.onNotify(method, msg["params"])
				}
				continue
			}
			// server->client request
			var id int
			_ = json.Unmarshal(idRaw, &id)
			if c.onRequest != nil {
				if res, err := c.onRequest(id, "", nil); err == nil {
					c.sendResult(id, res, nil)
					continue
				}
			}
			c.sendResult(id, nil, &rpcError{Code: -32601, Message: "not supported"})
			continue
		}
		// response to one of our requests
		var id int
		if err := json.Unmarshal(msg["id"], &id); err != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pend[id]
		delete(c.pend, id)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		if errRaw, ok := msg["error"]; ok {
			ch <- response{err: errors.New(string(errRaw))}
		} else {
			ch <- response{result: msg["result"]}
		}
	}
}

func (c *Client) sendResult(id int, result any, rpcErr *rpcError) {
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	b, _ := json.Marshal(msg)
	_ = c.write(b)
}
