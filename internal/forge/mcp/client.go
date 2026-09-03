// Package mcp is a minimal MCP (Model Context Protocol) client that speaks
// JSON-RPC 2.0 over Streamable HTTP — enough to talk to the BNK-Forge MCP
// server in its stateless mode (FastMCP with stateless_http=True,
// json_response=True).
//
// Each Call performs a fresh initialize → notifications/initialized →
// tools/call cycle, because the server is stateless.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// ProtocolVersion is the MCP protocol revision azrbnkctl targets.
const ProtocolVersion = "2024-11-05"

// Client is a minimal MCP-over-HTTP client.
type Client struct {
	endpoint  string
	http      *http.Client
	clientID  string
	idCounter atomic.Int64
}

// New returns a Client targeting the given MCP endpoint (e.g. "http://localhost:8081/mcp/").
func New(endpoint string, timeout time.Duration) *Client {
	return &Client{
		endpoint: endpoint,
		http:     &http.Client{Timeout: timeout},
		clientID: "azrbnkctl",
	}
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcErr) Error() string {
	return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message)
}

// ToolCallResult is the shape of an MCP tools/call result.
type ToolCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// Text returns the concatenated text of all text-typed content blocks.
func (r ToolCallResult) Text() string {
	var b bytes.Buffer
	for _, c := range r.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// CallTool performs init+notification+tools/call and returns the tool's result.
func (c *Client) CallTool(ctx context.Context, name string, args any) (ToolCallResult, error) {
	if c.endpoint == "" {
		return ToolCallResult{}, errors.New("mcp: endpoint is empty")
	}

	// 1) initialize handshake
	initParams, _ := json.Marshal(map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    c.clientID,
			"version": "1.0",
		},
	})
	if _, err := c.post(ctx, "initialize", initParams, true); err != nil {
		return ToolCallResult{}, fmt.Errorf("mcp init %s: %w", c.endpoint, err)
	}

	// 2) initialized notification
	_ = c.notify(ctx, "notifications/initialized", nil)

	// 3) tools/call
	toolParams, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return ToolCallResult{}, fmt.Errorf("marshal args: %w", err)
	}

	rawResult, err := c.post(ctx, "tools/call", toolParams, true)
	if err != nil {
		return ToolCallResult{}, fmt.Errorf("mcp %s: %w", name, err)
	}

	var res ToolCallResult
	if err := json.Unmarshal(rawResult, &res); err != nil {
		return ToolCallResult{}, fmt.Errorf("decode tool result: %w", err)
	}
	if res.IsError {
		return res, fmt.Errorf("mcp tool %s returned error: %s", name, res.Text())
	}
	return res, nil
}

func (c *Client) nextID() int64 {
	return c.idCounter.Add(1)
}

func (c *Client) post(ctx context.Context, method string, params json.RawMessage, needResp bool) (json.RawMessage, error) {
	reqObj := rpcReq{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	if needResp {
		reqObj.ID = c.nextID()
	}

	b, err := json.Marshal(reqObj)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	if !needResp {
		return nil, nil
	}

	var rpcRespObj rpcResp
	if err := json.NewDecoder(resp.Body).Decode(&rpcRespObj); err != nil {
		return nil, fmt.Errorf("decode json-rpc: %w", err)
	}
	if rpcRespObj.Error != nil {
		return nil, rpcRespObj.Error
	}
	return rpcRespObj.Result, nil
}

func (c *Client) notify(ctx context.Context, method string, params json.RawMessage) error {
	_, err := c.post(ctx, method, params, false)
	return err
}
