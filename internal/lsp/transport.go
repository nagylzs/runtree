package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
)

// The base protocol: messages with a Content-Length header, and a JSON-RPC body.

const maxMessageSize = 64 << 20

type transport struct {
	in    *bufio.Reader
	out   io.Writer
	outMu sync.Mutex
}

func (t *transport) read() (*message, error) {
	headers, err := textproto.NewReader(t.in).ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	length, err := strconv.Atoi(strings.TrimSpace(headers.Get("Content-Length")))
	if err != nil || length < 0 || length > maxMessageSize {
		return nil, fmt.Errorf("invalid Content-Length header: %q", headers.Get("Content-Length"))
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(t.in, body); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("invalid message: %w", err)
	}
	return &m, nil
}

func (t *transport) write(v interface{}) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	t.outMu.Lock()
	defer t.outMu.Unlock()
	if _, err := fmt.Fprintf(t.out, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = t.out.Write(body)
	return err
}

// respond sends the result of a request (a nil result is sent as null)
func (t *transport) respond(id json.RawMessage, result interface{}) error {
	return t.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  interface{}     `json:"result"`
	}{"2.0", id, result})
}

func (t *transport) respondError(id json.RawMessage, code int, msg string) error {
	return t.write(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   responseError   `json:"error"`
	}{"2.0", id, responseError{Code: code, Message: msg}})
}

func (t *transport) notify(method string, params interface{}) error {
	return t.write(struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params"`
	}{"2.0", method, params})
}
