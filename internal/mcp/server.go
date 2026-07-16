package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// ── MCP Protocol Constants ──

const ProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 types
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // nil = notification
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *ErrorObj   `json:"error,omitempty"`
}

type ErrorObj struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error codes
const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
	ErrNotInitialized = -32002
)

// ── Tool Definition ──

// ToolSchema defines an MCP tool's JSON Schema for parameters.
type ToolSchema struct {
	Type       string                  `json:"type"`
	Properties map[string]PropertySpec `json:"properties,omitempty"`
	Required   []string                `json:"required,omitempty"`
}

// PropertySpec defines a single parameter property.
type PropertySpec struct {
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Required    bool     `json:"-"`
}

// ToolDef is the MCP tool definition sent to the client.
type ToolDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema ToolSchema `json:"inputSchema"`
}

// ToolHandler is the function that executes a tool call.
type ToolHandler func(args map[string]interface{}) (interface{}, error)

// Server implements the MCP protocol over stdio.
type Server struct {
	name       string
	version    string
	tools      map[string]ToolDef
	handlers   map[string]ToolHandler
	mu         sync.Mutex
	initialized bool
	writer     io.Writer
	reader     *bufio.Scanner
	stopCh     chan struct{}
}

// NewServer creates a new MCP server.
func NewServer(name, version string) *Server {
	return &Server{
		name:     name,
		version:  version,
		tools:    make(map[string]ToolDef),
		handlers: make(map[string]ToolHandler),
		writer:   os.Stdout,
		reader:   bufio.NewScanner(os.Stdin),
		stopCh:   make(chan struct{}),
	}
}

// RegisterTool registers an MCP tool.
func (s *Server) RegisterTool(name, description string, schema ToolSchema, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[name] = ToolDef{
		Name:        name,
		Description: description,
		InputSchema: schema,
	}
	s.handlers[name] = handler
}

// SendNotification sends an MCP notification (no ID) to the client.
func (s *Server) SendNotification(method string, params interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintln(s.writer, string(data))
}

// SendEvent sends a custom event/notification.
func (s *Server) SendEvent(eventType string, data interface{}) {
	s.SendNotification("notifications/event", map[string]interface{}{
		"type": eventType,
		"data": data,
	})
}

// send responds to a JSON-RPC request.
func (s *Server) send(id interface{}, result interface{}, errObj *ErrorObj) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   errObj,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(s.writer, string(data))
}

// sendError is a convenience for error responses.
func (s *Server) sendError(id interface{}, code int, message string) {
	s.send(id, nil, &ErrorObj{Code: code, Message: message})
}

// handleMessage processes one JSON-RPC message.
func (s *Server) handleMessage(raw []byte) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		s.sendError(nil, ErrParse, "Parse error: "+err.Error())
		return
	}

	// Notifications have no ID
	isNotification := req.ID == nil || len(req.ID) == 0

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
		}
		if req.Params != nil {
			json.Unmarshal(req.Params, &params)
		}
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()

		if !isNotification {
			s.send(req.ID, map[string]interface{}{
				"protocolVersion": ProtocolVersion,
				"serverInfo": map[string]string{
					"name":    s.name,
					"version": s.version,
				},
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
			}, nil)
		}

	case "notifications/initialized":
		// Client confirms initialization — no response needed

	case "tools/list":
		if !s.initialized {
			s.sendError(req.ID, ErrNotInitialized, "Server not initialized")
			return
		}
		s.mu.Lock()
		toolList := make([]ToolDef, 0, len(s.tools))
		for _, t := range s.tools {
			toolList = append(toolList, t)
		}
		s.mu.Unlock()
		if !isNotification {
			s.send(req.ID, map[string]interface{}{
				"tools": toolList,
			}, nil)
		}

	case "tools/call":
		if !s.initialized {
			s.sendError(req.ID, ErrNotInitialized, "Server not initialized")
			return
		}
		var params struct {
			Name    string                 `json:"name"`
			Args    map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, ErrInvalidParams, "Invalid params: "+err.Error())
			return
		}

		s.mu.Lock()
		handler, ok := s.handlers[params.Name]
		s.mu.Unlock()

		if !ok {
			s.sendError(req.ID, ErrMethodNotFound, "Unknown tool: "+params.Name)
			return
		}

		result, err := handler(params.Args)
		if err != nil {
			s.sendError(req.ID, ErrInternal, err.Error())
			return
		}

		// MCP tool result format — marshal to JSON for MCP client compatibility
		if !isNotification {
			resultJSON, marshalErr := json.Marshal(result)
			resultStr := ""
			if marshalErr != nil {
				resultStr = fmt.Sprintf(`{"error":"marshal failed: %v"}`, marshalErr)
			} else {
				resultStr = string(resultJSON)
			}
			s.send(req.ID, map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": resultStr},
				},
			}, nil)
		}

	default:
		if !isNotification {
			s.sendError(req.ID, ErrMethodNotFound, "Unknown method: "+req.Method)
		}
	}
}

// ListenStdio starts the stdio listener loop.
func (s *Server) ListenStdio() {
	// Signal readiness
	s.SendNotification("notifications/ready", map[string]string{
		"name":    s.name,
		"version": s.version,
	})

	s.reader.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.reader.Scan() {
		line := s.reader.Text()
		if line == "" {
			continue
		}
		s.handleMessage([]byte(line))
	}

	if err := s.reader.Err(); err != nil {
		log.Printf("MCP server read error: %v", err)
	}
}
