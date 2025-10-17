package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/steveyegge/beads/internal/storage"
)

// Server is the RPC server that handles requests from bd clients.
type Server struct {
	storage  storage.Storage
	listener net.Listener
	sockPath string
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex // Protects shutdown state
	shutdown bool
	handlers map[string]func(context.Context, *Request) *Response
}

// NewServer creates a new RPC server.
func NewServer(store storage.Storage, sockPath string) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		storage:  store,
		sockPath: sockPath,
		ctx:      ctx,
		cancel:   cancel,
	}
	s.initHandlers()
	return s
}

// initHandlers initializes the operation handler map.
func (s *Server) initHandlers() {
	s.handlers = map[string]func(context.Context, *Request) *Response{
		OpBatch:        s.handleBatch,
		OpCreate:       s.handleCreate,
		OpUpdate:       s.handleUpdate,
		OpClose:        s.handleClose,
		OpList:         s.handleList,
		OpShow:         s.handleShow,
		OpReady:        s.handleReady,
		OpBlocked:      s.handleBlocked,
		OpStats:        s.handleStats,
		OpDepAdd:       s.handleDepAdd,
		OpDepRemove:    s.handleDepRemove,
		OpDepTree:      s.handleDepTree,
		OpLabelAdd:     s.handleLabelAdd,
		OpLabelRemove:  s.handleLabelRemove,
		OpLabelList:    s.handleLabelList,
		OpLabelListAll: s.handleLabelListAll,
	}
}

// Start starts the RPC server listening on the Unix socket.
func (s *Server) Start() error {
	if err := os.Remove(s.sockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", s.sockPath, err)
	}
	s.listener = listener

	if err := os.Chmod(s.sockPath, 0600); err != nil {
		s.listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop gracefully stops the RPC server.
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return nil
	}
	s.shutdown = true
	s.mu.Unlock()

	s.cancel()

	if s.listener != nil {
		s.listener.Close()
	}

	s.wg.Wait()

	if err := os.Remove(s.sockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove socket: %w", err)
	}

	return nil
}

// acceptLoop accepts incoming connections and handles them.
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				fmt.Fprintf(os.Stderr, "Error accepting connection: %v\n", err)
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection handles a single client connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	writer := bufio.NewWriter(conn)

	for scanner.Scan() {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := NewErrorResponse(fmt.Errorf("invalid request JSON: %w", err))
			s.sendResponse(writer, resp)
			continue
		}

		resp := s.handleRequest(&req)
		s.sendResponse(writer, resp)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading from connection: %v\n", err)
	}
}

// sendResponse sends a response to the client.
func (s *Server) sendResponse(writer *bufio.Writer, resp *Response) {
	respJSON, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling response: %v\n", err)
		return
	}

	if _, err := writer.Write(respJSON); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing response: %v\n", err)
		return
	}
	if _, err := writer.Write([]byte("\n")); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing newline: %v\n", err)
		return
	}
	if err := writer.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "Error flushing response: %v\n", err)
	}
}

// handleRequest processes an RPC request and returns a response.
func (s *Server) handleRequest(req *Request) *Response {
	ctx := context.Background()

	handler, ok := s.handlers[req.Operation]
	if !ok {
		return NewErrorResponse(fmt.Errorf("unknown operation: %s", req.Operation))
	}

	return handler(ctx, req)
}

// Placeholder handlers - will be implemented in future commits
func (s *Server) handleBatch(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("batch operation not yet implemented"))
}

func (s *Server) handleCreate(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("create operation not yet implemented"))
}

func (s *Server) handleUpdate(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("update operation not yet implemented"))
}

func (s *Server) handleClose(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("close operation not yet implemented"))
}

func (s *Server) handleList(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("list operation not yet implemented"))
}

func (s *Server) handleShow(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("show operation not yet implemented"))
}

func (s *Server) handleReady(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("ready operation not yet implemented"))
}

func (s *Server) handleBlocked(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("blocked operation not yet implemented"))
}

func (s *Server) handleStats(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("stats operation not yet implemented"))
}

func (s *Server) handleDepAdd(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("dep_add operation not yet implemented"))
}

func (s *Server) handleDepRemove(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("dep_remove operation not yet implemented"))
}

func (s *Server) handleDepTree(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("dep_tree operation not yet implemented"))
}

func (s *Server) handleLabelAdd(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("label_add operation not yet implemented"))
}

func (s *Server) handleLabelRemove(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("label_remove operation not yet implemented"))
}

func (s *Server) handleLabelList(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("label_list operation not yet implemented"))
}

func (s *Server) handleLabelListAll(_ context.Context, _ *Request) *Response {
	return NewErrorResponse(fmt.Errorf("label_list_all operation not yet implemented"))
}
