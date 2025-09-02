package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Server interface defines the contract for backend servers
type Server interface {
	Address() string
	IsAlive() bool
	Serve(rw http.ResponseWriter, r *http.Request)
	SetAlive(alive bool)
	GetConnections() int
	IncrementConnections()
	DecrementConnections()
}

// simpleServer implements the Server interface
type simpleServer struct {
	addr        string
	proxy       *httputil.ReverseProxy
	alive       bool
	mutex       sync.RWMutex
	connections int
}

// LoadBalancerStats holds statistics for monitoring
type LoadBalancerStats struct {
	TotalRequests  int64            `json:"total_requests"`
	FailedRequests int64            `json:"failed_requests"`
	ServerStats    map[string]int64 `json:"server_stats"`
	Uptime         string           `json:"uptime"`
	ActiveServers  int              `json:"active_servers"`
	TotalServers   int              `json:"total_servers"`
}

// newSimpleServer creates a new server instance with enhanced features
func newSimpleServer(addr string) *simpleServer {
	serverUrl, err := url.Parse(addr)
	if err != nil {
		log.Printf("Error parsing server URL %s: %v", addr, err)
		return nil
	}

	proxy := httputil.NewSingleHostReverseProxy(serverUrl)

	// Enhanced error handler with detailed logging
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		log.Printf("Proxy error for %s: %v", addr, err)
		http.Error(rw, "Service Temporarily Unavailable", http.StatusBadGateway)
	}

	// Custom director to properly handle requests
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = serverUrl.Host
		req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
		req.Header.Set("X-Real-IP", req.RemoteAddr)
	}

	server := &simpleServer{
		addr:        addr,
		proxy:       proxy,
		alive:       true,
		connections: 0,
	}

	return server
}

// Server interface implementations
func (s *simpleServer) Address() string {
	return s.addr
}

func (s *simpleServer) IsAlive() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.alive
}

func (s *simpleServer) SetAlive(alive bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.alive = alive
	if !alive {
		log.Printf("Server %s marked as DOWN", s.addr)
	} else {
		log.Printf("Server %s marked as UP", s.addr)
	}
}

func (s *simpleServer) GetConnections() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.connections
}

func (s *simpleServer) IncrementConnections() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.connections++
}

func (s *simpleServer) DecrementConnections() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.connections > 0 {
		s.connections--
	}
}

func (s *simpleServer) Serve(rw http.ResponseWriter, req *http.Request) {
	s.IncrementConnections()
	defer s.DecrementConnections()
	s.proxy.ServeHTTP(rw, req)
}

// LoadBalancer manages multiple backend servers
type LoadBalancer struct {
	port            string
	roundRobinCount int
	servers         []Server
	mutex           sync.RWMutex
	stats           LoadBalancerStats
	startTime       time.Time
	serverStats     map[string]int64
}

// NewLoadBalancer creates a new load balancer instance
func NewLoadBalancer(port string, servers []Server) *LoadBalancer {
	return &LoadBalancer{
		port:            port,
		roundRobinCount: 0,
		servers:         servers,
		stats:           LoadBalancerStats{ServerStats: make(map[string]int64)},
		startTime:       time.Now(),
		serverStats:     make(map[string]int64),
	}
}

// getNextAvailableServer implements round-robin with health checking
func (lb *LoadBalancer) getNextAvailableServer() Server {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	// Count healthy servers
	healthyServers := 0
	for _, server := range lb.servers {
		if server.IsAlive() {
			healthyServers++
		}
	}

	if healthyServers == 0 {
		log.Println("WARNING: No healthy servers available!")
		return nil
	}

	// Find next available server
	attempts := 0
	for attempts < len(lb.servers) {
		server := lb.servers[lb.roundRobinCount%len(lb.servers)]
		lb.roundRobinCount++

		if server.IsAlive() {
			// Update stats
			lb.serverStats[server.Address()]++
			return server
		}
		attempts++
	}

	return nil
}

// healthCheck performs periodic health checks on all servers
func (lb *LoadBalancer) healthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Performing health checks...")

		for _, server := range lb.servers {
			go func(s Server) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				// Create a simple health check request
				req, err := http.NewRequestWithContext(ctx, "GET", s.Address(), nil)
				if err != nil {
					s.SetAlive(false)
					return
				}

				client := &http.Client{Timeout: 10 * time.Second}
				resp, err := client.Do(req)
				if err != nil {
					s.SetAlive(false)
					return
				}
				defer resp.Body.Close()

				// Consider server alive if status code is not 5xx
				if resp.StatusCode < 500 {
					s.SetAlive(true)
				} else {
					s.SetAlive(false)
				}
			}(server)
		}
	}
}

// updateStats updates load balancer statistics
func (lb *LoadBalancer) updateStats(success bool) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	lb.stats.TotalRequests++
	if !success {
		lb.stats.FailedRequests++
	}
}

// getStats returns current load balancer statistics
func (lb *LoadBalancer) getStats() LoadBalancerStats {
	lb.mutex.RLock()
	defer lb.mutex.RUnlock()

	activeServers := 0
	for _, server := range lb.servers {
		if server.IsAlive() {
			activeServers++
		}
	}

	stats := lb.stats
	stats.Uptime = time.Since(lb.startTime).String()
	stats.ActiveServers = activeServers
	stats.TotalServers = len(lb.servers)
	stats.ServerStats = make(map[string]int64)

	for addr, count := range lb.serverStats {
		stats.ServerStats[addr] = count
	}

	return stats
}

// serveProxy handles incoming requests and forwards them to backend servers
func (lb *LoadBalancer) serveProxy(rw http.ResponseWriter, req *http.Request) {
	targetServer := lb.getNextAvailableServer()

	if targetServer == nil {
		log.Println("No healthy servers available")
		lb.updateStats(false)
		http.Error(rw, "Service Unavailable - All servers are down", http.StatusServiceUnavailable)
		return
	}

	log.Printf("Forwarding request to %s [Connections: %d]",
		targetServer.Address(), targetServer.GetConnections())

	// Add request timing
	start := time.Now()
	targetServer.Serve(rw, req)
	duration := time.Since(start)

	lb.updateStats(true)
	log.Printf("Request completed in %v", duration)
}

// statsHandler provides load balancer statistics
func (lb *LoadBalancer) statsHandler(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("Access-Control-Allow-Origin", "*")

	stats := lb.getStats()
	json.NewEncoder(rw).Encode(stats)
}

// enableCORS middleware for cross-origin requests
func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func main() {
	log.Println("🚀 Starting Enhanced HTTP Load Balancer...")

	// Initialize backend servers - using real endpoints for testing
	servers := []Server{
		newSimpleServer("https://httpbin.org"),                  // HTTP testing service
		newSimpleServer("https://jsonplaceholder.typicode.com"), // JSON API service
		newSimpleServer("https://httpstat.us"),                  // HTTP status service
		newSimpleServer("http://localhost:8001"),
		newSimpleServer("http://localhost:8002"),
		newSimpleServer("http://localhost:8003"),
	}

	// Filter out nil servers (in case of URL parsing errors)
	var validServers []Server
	for _, server := range servers {
		if server != nil {
			validServers = append(validServers, server)
			log.Printf("Added backend server: %s", server.Address())
		}
	}

	if len(validServers) == 0 {
		log.Fatal("No valid backend servers configured")
	}

	// Create load balancer
	lb := NewLoadBalancer("8000", validServers)

	// Start health checking in background
	go lb.healthCheck()

	// Setup HTTP handlers
	http.HandleFunc("/", enableCORS(lb.serveProxy))
	http.HandleFunc("/stats", enableCORS(lb.statsHandler))
	http.HandleFunc("/health", enableCORS(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
			"active_servers": func() int {
				count := 0
				for _, server := range lb.servers {
					if server.IsAlive() {
						count++
					}
				}
				return count
			}(),
		}
		json.NewEncoder(rw).Encode(response)
	}))

	// Setup graceful shutdown
	server := &http.Server{
		Addr:    ":" + lb.port,
		Handler: nil,
	}

	// Handle shutdown signals
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down load balancer...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		log.Println("Load balancer stopped")
		os.Exit(0)
	}()

	log.Printf("🌐 Load Balancer running on http://localhost:%s", lb.port)
	log.Printf("📊 Statistics available at http://localhost:%s/stats", lb.port)
	log.Printf("❤️  Health check at http://localhost:%s/health", lb.port)
	log.Println("Press Ctrl+C to stop...")

	// Start the server
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}
