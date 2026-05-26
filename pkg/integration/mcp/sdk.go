package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type NetworkManager struct {
	mu      sync.Mutex
	baseURL string
	server  *http.Server
	items   map[string]*mcpsdk.Server
}

type NetworkEndpoint struct {
	URL     string
	cleanup func()
}

func (e NetworkEndpoint) Close() {
	if e.cleanup != nil {
		e.cleanup()
	}
}

var (
	defaultNetworkMu sync.Mutex
	defaultNetwork   *NetworkManager
)

func DefaultNetworkManager() (*NetworkManager, error) {
	defaultNetworkMu.Lock()
	defer defaultNetworkMu.Unlock()
	if defaultNetwork != nil {
		return defaultNetwork, nil
	}
	manager, err := NewNetworkManager()
	if err != nil {
		return nil, err
	}
	defaultNetwork = manager
	return manager, nil
}

func NewNetworkManager() (*NetworkManager, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	manager := &NetworkManager{
		baseURL: "http://" + ln.Addr().String(),
		items:   map[string]*mcpsdk.Server{},
	}
	handler := mcpsdk.NewStreamableHTTPHandler(manager.serverForRequest, nil)
	manager.server = &http.Server{Handler: handler}
	go func() {
		_ = manager.server.Serve(ln)
	}()
	return manager, nil
}

func (m *NetworkManager) Register(server *mcpsdk.Server) (NetworkEndpoint, error) {
	token, err := randomToken()
	if err != nil {
		return NetworkEndpoint{}, err
	}
	m.mu.Lock()
	m.items[token] = server
	m.mu.Unlock()
	return NetworkEndpoint{
		URL: m.baseURL + "/mcp/" + token,
		cleanup: func() {
			m.mu.Lock()
			delete(m.items, token)
			m.mu.Unlock()
		},
	}, nil
}

func (m *NetworkManager) serverForRequest(req *http.Request) *mcpsdk.Server {
	token := strings.TrimPrefix(req.URL.Path, "/mcp/")
	if token == "" || strings.Contains(token, "/") {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.items[token]
}

func randomToken() (string, error) {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
