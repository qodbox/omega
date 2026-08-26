package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const (
	protocolVersion = "2025-06-18"
	serverName      = "omega"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`

	Run func(args map[string]any) (string, error) `json:"-"`
}

type Server struct {
	version string
	tools   []Tool
	index   map[string]Tool

	mu  sync.Mutex
	out *json.Encoder
}

func New(version string, tools []Tool) *Server {
	index := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		index[tool.Name] = tool
	}
	return &Server{version: version, tools: tools, index: index}
}

func (s *Server) Serve(in io.Reader, out io.Writer) error {
	s.out = json.NewEncoder(out)

	reader := bufio.NewReaderSize(in, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			s.handle(line)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (s *Server) handle(line []byte) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}

	if len(req.ID) == 0 {
		return
	}

	switch req.Method {
	case "initialize":
		s.reply(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": s.version},
			"instructions": "Omega is a Go JSON API framework (Fiber, GORM, REST + OpenAPI + GraphQL, JWT). " +
				"Call omega_overview first: it explains the layout and the conventions every other tool assumes.",
		})
	case "ping":
		s.reply(req.ID, map[string]any{})
	case "tools/list":
		s.reply(req.ID, map[string]any{"tools": s.tools})
	case "tools/call":
		s.call(req)
	default:
		s.fail(req.ID, -32601, "unknown method "+req.Method)
	}
}

func (s *Server) call(req request) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.fail(req.ID, -32602, "invalid parameters")
		return
	}

	tool, ok := s.index[params.Name]
	if !ok {
		s.fail(req.ID, -32602, "unknown tool "+params.Name)
		return
	}

	text, err := tool.Run(params.Arguments)
	if err != nil {
		s.reply(req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		})
		return
	}

	s.reply(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

func (s *Server) reply(id json.RawMessage, result any) {
	s.write(response{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) fail(id json.RawMessage, code int, message string) {
	s.write(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

func (s *Server) write(msg response) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.out.Encode(msg); err != nil {
		fmt.Fprintln(io.Discard, err)
	}
}

func Schema(properties map[string]string, required ...string) map[string]any {
	props := map[string]any{}
	for name, description := range properties {
		props[name] = map[string]any{"type": "string", "description": description}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
