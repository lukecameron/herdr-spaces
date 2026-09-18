// Package herdr is the small part of the Herdr socket API this plugin uses.
package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"
)

// Client speaks newline-delimited JSON to the Herdr socket. Herdr closes the
// connection after each answer, so every call dials its own.
type Client struct {
	path string
	seq  atomic.Uint64
}

// NewFromEnv builds a client for the socket Herdr names in HERDR_SOCKET_PATH.
func NewFromEnv() (*Client, error) {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		return nil, fmt.Errorf("HERDR_SOCKET_PATH is not set; this must be started by Herdr")
	}
	return &Client{path: path}, nil
}

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type frame struct {
	Result json.RawMessage `json:"result"`
	Error  *APIError       `json:"error"`
}

// APIError is the error object Herdr answers with.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("herdr api error %s: %s", e.Code, e.Message)
}

// Call sends one request and decodes the result into result when it is not nil.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if params == nil {
		params = struct{}{}
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return fmt.Errorf("connect to herdr socket %s: %w", c.path, err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	req := request{ID: fmt.Sprintf("spaces-%d", c.seq.Add(1)), Method: method, Params: params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return ctxErr(ctx, fmt.Errorf("send %s: %w", method, err))
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return ctxErr(ctx, fmt.Errorf("read %s response: %w", method, err))
	}

	var f frame
	if err := json.Unmarshal(line, &f); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if f.Error != nil {
		return fmt.Errorf("%s: %w", method, f.Error)
	}
	if result != nil && len(f.Result) > 0 {
		if err := json.Unmarshal(f.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

func ctxErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// Snapshot fetches the whole session in one call.
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var res struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.Call(ctx, "session.snapshot", nil, &res); err != nil {
		return Snapshot{}, err
	}
	return res.Snapshot, nil
}

// RenameWorkspace sets a workspace label.
func (c *Client) RenameWorkspace(ctx context.Context, workspaceID, label string) error {
	params := struct {
		WorkspaceID string `json:"workspace_id"`
		Label       string `json:"label"`
	}{workspaceID, label}
	return c.Call(ctx, "workspace.rename", params, nil)
}

// ReportWorkspaceTokens patches display tokens on a workspace. A nil value
// clears that token. A zero ttl reports without expiry.
func (c *Client) ReportWorkspaceTokens(ctx context.Context, workspaceID, source string, tokens map[string]*string, ttl time.Duration) error {
	params := struct {
		WorkspaceID string             `json:"workspace_id"`
		Source      string             `json:"source"`
		Tokens      map[string]*string `json:"tokens"`
		TTLMs       *int64             `json:"ttl_ms,omitempty"`
	}{WorkspaceID: workspaceID, Source: source, Tokens: tokens}
	if ttl > 0 {
		ms := ttl.Milliseconds()
		params.TTLMs = &ms
	}
	return c.Call(ctx, "workspace.report_metadata", params, nil)
}
