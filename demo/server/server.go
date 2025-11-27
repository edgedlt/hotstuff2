package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/edgedlt/hotstuff2/demo/simulator"
)

type Server struct {
	mu        sync.RWMutex
	sim       *simulator.Simulator
	mux       *http.ServeMux
	staticDir string
}

func New(staticDir string) *Server {
	s := &Server{
		mux:       http.NewServeMux(),
		staticDir: staticDir,
	}

	// Create initial simulator
	sim, err := simulator.New(simulator.DefaultConfig())
	if err != nil {
		log.Printf("Warning: failed to create initial simulator: %v", err)
	}
	s.sim = sim

	// Register routes
	s.registerRoutes()

	return s
}

// registerRoutes sets up all HTTP routes.
func (s *Server) registerRoutes() {
	// API routes
	s.mux.HandleFunc("/api/state", s.handleGetState)
	s.mux.HandleFunc("/api/start", s.handleStart)
	s.mux.HandleFunc("/api/stop", s.handleStop)
	s.mux.HandleFunc("/api/reset", s.handleReset)
	s.mux.HandleFunc("/api/level", s.handleSetLevel)
	s.mux.HandleFunc("/api/crash", s.handleCrashNode)
	s.mux.HandleFunc("/api/recover", s.handleRecoverNode)
	s.mux.HandleFunc("/api/partition", s.handleSetPartition)
	s.mux.HandleFunc("/api/heal", s.handleHealAll)

	// Static files
	fs := http.FileServer(http.Dir(s.staticDir))
	s.mux.Handle("/", fs)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for development
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.mux.ServeHTTP(w, r)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	log.Printf("Starting HotStuff-2 Demo server on %s", addr)
	log.Printf("Open http://localhost%s in your browser", addr)
	return http.ListenAndServe(addr, s)
}

// handleGetState returns the current simulation state.
func (s *Server) handleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	sim := s.sim
	s.mu.RUnlock()

	if sim == nil {
		http.Error(w, "Simulator not initialized", http.StatusInternalServerError)
		return
	}

	state := sim.GetState()
	s.writeJSON(w, state)
}

// handleStart starts the simulation.
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	sim := s.sim
	s.mu.Unlock()

	if sim == nil {
		http.Error(w, "Simulator not initialized", http.StatusInternalServerError)
		return
	}

	if err := sim.Start(); err != nil {
		s.writeError(w, err)
		return
	}

	s.writeJSON(w, map[string]string{"status": "started"})
}

// handleStop stops the simulation.
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	sim := s.sim
	s.mu.Unlock()

	if sim == nil {
		http.Error(w, "Simulator not initialized", http.StatusInternalServerError)
		return
	}

	sim.Stop()
	s.writeJSON(w, map[string]string{"status": "stopped"})
}

// handleReset resets the simulation.
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sim != nil {
		s.sim.HealAll() // Clear all faults before reset
		s.sim.Stop()
	}

	// Get level from query params
	levelStr := r.URL.Query().Get("level")
	level := simulator.LevelHappyPath
	if levelStr != "" {
		switch levelStr {
		case "0", "happy":
			level = simulator.LevelHappyPath
		case "1", "byzantine":
			level = simulator.LevelByzantine
		case "2", "chaos":
			level = simulator.LevelChaos
		}
	}

	// Get fault tolerance from query params (f value, nodes = 3f+1)
	faultTolerance := 1
	if f := r.URL.Query().Get("f"); f != "" {
		if fVal, err := strconv.Atoi(f); err == nil && fVal >= 1 && fVal <= simulator.MaxFaultTolerance {
			faultTolerance = fVal
		}
	}

	// Get block time mode from query params
	blockTimeMode := simulator.BlockTime1Second // Default to 1-second blocks for visibility
	if btm := r.URL.Query().Get("blocktime"); btm != "" {
		switch btm {
		case "0", "optimistic":
			blockTimeMode = simulator.BlockTimeOptimistic
		case "1", "1s":
			blockTimeMode = simulator.BlockTime1Second
		}
	}

	// Create new simulator
	cfg := simulator.Config{
		Level:          level,
		FaultTolerance: faultTolerance,
		BlockTimeMode:  blockTimeMode,
		Seed:           42,
	}

	sim, err := simulator.New(cfg)
	if err != nil {
		s.writeError(w, err)
		return
	}

	s.sim = sim
	s.writeJSON(w, map[string]interface{}{
		"status":         "reset",
		"level":          level.String(),
		"faultTolerance": faultTolerance,
		"nodeCount":      cfg.NodeCount(),
	})
}

// handleSetLevel sets the simulation level.
func (s *Server) handleSetLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	levelStr := r.URL.Query().Get("value")
	if levelStr == "" {
		http.Error(w, "Missing level value", http.StatusBadRequest)
		return
	}

	var level simulator.Level
	switch levelStr {
	case "0", "happy":
		level = simulator.LevelHappyPath
	case "1", "byzantine":
		level = simulator.LevelByzantine
	case "2", "chaos":
		level = simulator.LevelChaos
	default:
		http.Error(w, "Invalid level value", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if s.sim != nil {
		s.sim.Stop()
		s.sim.SetLevel(level)
		s.sim.Reset()
	}
	s.mu.Unlock()

	s.writeJSON(w, map[string]string{"level": level.String()})
}

// handleCrashNode crashes a node.
func (s *Server) handleCrashNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeStr := r.URL.Query().Get("node")
	if nodeStr == "" {
		http.Error(w, "Missing node parameter", http.StatusBadRequest)
		return
	}

	nodeID, err := strconv.Atoi(nodeStr)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	sim := s.sim
	s.mu.RUnlock()

	if sim == nil {
		http.Error(w, "Simulator not initialized", http.StatusInternalServerError)
		return
	}

	if err := sim.CrashNode(nodeID); err != nil {
		s.writeError(w, err)
		return
	}

	s.writeJSON(w, map[string]interface{}{"node": nodeID, "status": "crashed"})
}

// handleRecoverNode recovers a crashed node.
func (s *Server) handleRecoverNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeStr := r.URL.Query().Get("node")
	if nodeStr == "" {
		http.Error(w, "Missing node parameter", http.StatusBadRequest)
		return
	}

	nodeID, err := strconv.Atoi(nodeStr)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	sim := s.sim
	s.mu.RUnlock()

	if sim == nil {
		http.Error(w, "Simulator not initialized", http.StatusInternalServerError)
		return
	}

	if err := sim.RecoverNode(nodeID); err != nil {
		s.writeError(w, err)
		return
	}

	s.writeJSON(w, map[string]interface{}{"node": nodeID, "status": "recovered"})
}

// handleSetPartition sets network partitions.
func (s *Server) handleSetPartition(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse partition from request body
	var req struct {
		Partitions [][]int `json:"partitions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If no body, clear partitions
		s.mu.RLock()
		sim := s.sim
		s.mu.RUnlock()

		if sim != nil {
			sim.ClearPartitions()
		}

		s.writeJSON(w, map[string]string{"status": "partitions cleared"})
		return
	}

	s.mu.RLock()
	sim := s.sim
	s.mu.RUnlock()

	if sim == nil {
		http.Error(w, "Simulator not initialized", http.StatusInternalServerError)
		return
	}

	sim.SetPartitions(req.Partitions)
	s.writeJSON(w, map[string]interface{}{"partitions": req.Partitions})
}

// handleHealAll recovers all crashed nodes and clears partitions.
func (s *Server) handleHealAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	sim := s.sim
	s.mu.RUnlock()

	if sim == nil {
		http.Error(w, "Simulator not initialized", http.StatusInternalServerError)
		return
	}

	sim.HealAll()
	s.writeJSON(w, map[string]string{"status": "all healed"})
}

// writeJSON writes a JSON response.
func (s *Server) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// writeError writes an error response.
func (s *Server) writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("%v", err)})
}
