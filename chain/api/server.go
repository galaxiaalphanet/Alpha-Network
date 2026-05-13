// Package api provides the HTTP REST API for Alpha Network
// Designed for AI agents: pure JSON, no wallet UI, no browser required
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alpha-network/alpha/chain/agent"
	"github.com/alpha-network/alpha/chain/core"
	alphacrypto "github.com/alpha-network/alpha/chain/crypto"
	"github.com/alpha-network/alpha/chain/data"
	"github.com/alpha-network/alpha/chain/ledger"
	"github.com/alpha-network/alpha/chain/monitor"
	"github.com/alpha-network/alpha/chain/net"
	"github.com/alpha-network/alpha/chain/p2p"
	"github.com/alpha-network/alpha/chain/producer"
	"github.com/alpha-network/alpha/chain/sync"
	"github.com/alpha-network/alpha/chain/tasks"
)

// Server is the Alpha Network REST API server
type Server struct {
	registry    *agent.Registry
	ledger      *ledger.Ledger
	producer    *producer.BlockProducer
	oracle      *data.IntelligenceOracle
	marketplace *tasks.Marketplace
	hub         *net.Hub
	mon         *monitor.Monitor
	rl          *RateLimiter
	port        int
	mux         *http.ServeMux
	peerStore   *p2p.PeerStore
	syncer      *sync.Syncer
}

// NewServer creates an API server
// ledger, prod, and oracle may be nil for backward-compatibility; non-nil enables the extended endpoints.
func NewServer(registry *agent.Registry, port int) *Server {
	s := &Server{
		registry: registry,
		rl:       NewRateLimiter(),
		port:     port,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

// NewServerFull creates an API server with all subsystems wired up
func NewServerFull(
	registry *agent.Registry,
	l *ledger.Ledger,
	prod *producer.BlockProducer,
	oracle *data.IntelligenceOracle,
	port int,
) *Server {
	s := &Server{
		registry: registry,
		ledger:   l,
		producer: prod,
		oracle:   oracle,
		rl:       NewRateLimiter(),
		port:     port,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

// NewServerPhase2 creates an API server with all Phase 2 subsystems wired up.
func NewServerPhase2(
	registry *agent.Registry,
	l *ledger.Ledger,
	prod *producer.BlockProducer,
	oracle *data.IntelligenceOracle,
	mp *tasks.Marketplace,
	hub *net.Hub,
	port int,
) *Server {
	s := &Server{
		registry:    registry,
		ledger:      l,
		producer:    prod,
		oracle:      oracle,
		marketplace: mp,
		hub:         hub,
		rl:          NewRateLimiter(),
		port:        port,
		mux:         http.NewServeMux(),
		peerStore:   p2p.NewPeerStore(),
		syncer:      sync.NewSyncer(),
	}
	s.routes()
	return s
}

// SetPeerStore replaces the default peer store (e.g., pre-loaded from disk).
func (s *Server) SetPeerStore(ps *p2p.PeerStore) {
	s.peerStore = ps
}

// SetMonitor attaches a health monitor to the server (optional).
func (s *Server) SetMonitor(m *monitor.Monitor) {
	s.mon = m
}

// Start launches the API server
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Alpha Network API listening on %s", addr)
	// Wrap mux with rate-limiting middleware
	handler := s.rl.Middleware(s.mux)
	return http.ListenAndServe(addr, handler)
}

// routes registers all API endpoints
func (s *Server) routes() {
	// Health
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/v1/health/detailed", s.handleHealthDetailed)

	// WebSocket streaming
	s.mux.HandleFunc("/ws", s.handleWebSocket)

	// Agent registry
	s.mux.HandleFunc("/api/v1/agents/register", s.handleAgentRegister)
	s.mux.HandleFunc("/api/v1/agents/", s.handleAgentGet)
	s.mux.HandleFunc("/api/v1/agents", s.handleAgentList)

	// Transfers
	s.mux.HandleFunc("/api/v1/transfer", s.handleTransfer)

	// Tasks (Phase 2)
	s.mux.HandleFunc("/api/v1/tasks/available", s.handleTasksAvailable)
	s.mux.HandleFunc("/api/v1/tasks/post", s.handleTaskPost)
	s.mux.HandleFunc("/api/v1/tasks/", s.handleTaskByID) // covers /tasks/{id} and /tasks/{id}/submit
	s.mux.HandleFunc("/api/v1/tasks", s.handleTaskList)

	// Chain info
	s.mux.HandleFunc("/api/v1/chain/info", s.handleChainInfo)

	// P2P peers (Phase 4)
	s.mux.HandleFunc("/api/v1/peers", s.handlePeerList)
	s.mux.HandleFunc("/api/v1/peers/announce", s.handlePeerAnnounce)

	// Block sync (Phase 4)
	s.mux.HandleFunc("/api/v1/sync/status", s.handleSyncStatus)

	// ZK Proof endpoint (Phase 4)
	s.mux.HandleFunc("/api/v1/proof/poi", s.handlePoIProof)

	// --- v0.2 endpoints ---

	// Intelligence layer
	s.mux.HandleFunc("/api/v1/intelligence/query", s.handleIntelligenceQuery)
	s.mux.HandleFunc("/api/v1/intelligence/stats", s.handleIntelligenceStats)
	s.mux.HandleFunc("/api/v1/intelligence/top", s.handleIntelligenceTop)

	// Account ledger
	s.mux.HandleFunc("/api/v1/accounts/", s.handleAccountBalance)

	// Blocks
	s.mux.HandleFunc("/api/v1/blocks/latest", s.handleBlockLatest)
	s.mux.HandleFunc("/api/v1/blocks/", s.handleBlockByHeight)
}

// --- Handlers (original) ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	height := uint64(0)
	if s.producer != nil {
		height = s.producer.GetChainHeight()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"chain":     "alpha-1",
		"timestamp": time.Now().Unix(),
		"version":   "0.3.0",
		"height":    height,
	})
}

// POST /api/v1/agents/register
func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req struct {
		Address      core.Address      `json:"address"`
		Capabilities []core.Capability `json:"capabilities"`
		Stake        core.Amount       `json:"stake"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Address == "" {
		writeError(w, http.StatusBadRequest, "address required")
		return
	}

	if len(req.Capabilities) == 0 {
		req.Capabilities = []core.Capability{core.CapabilityValidation}
	}

	blockHeight := uint64(0)
	if s.producer != nil {
		blockHeight = s.producer.GetChainHeight()
	} else {
		blockHeight = uint64(time.Now().Unix())
	}

	identity, err := s.registry.RegisterAgent(req.Address, req.Capabilities, req.Stake, blockHeight)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Credit initial stake to agent's ledger account
	if s.ledger != nil && req.Stake > 0 {
		agentAddr := core.Address("alpha_agent_" + string(identity.AgentID))
		_ = s.ledger.Credit(agentAddr, req.Stake)
	}

	// Update producer agent count
	if s.producer != nil {
		s.producer.SetAgentCount(len(s.registry.ListAgents(nil)))
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"agent_id": identity.AgentID,
		"identity": identity,
		"message":  "Agent registered on Alpha Network. Start earning $ALPHA.",
	})
}

// GET /api/v1/agents/{agent_id}
func (s *Server) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	agentID := core.AgentID(r.URL.Path[len("/api/v1/agents/"):])
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id required")
		return
	}

	identity, err := s.registry.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	trust, _ := s.registry.TrustScore(agentID)

	resp := map[string]interface{}{
		"identity":    identity,
		"trust_score": trust,
	}

	// Include ledger balance if available
	if s.ledger != nil {
		agentAddr := core.Address("alpha_agent_" + string(agentID))
		resp["balance"] = s.ledger.Balance(agentAddr)
	}

	writeJSON(w, http.StatusOK, resp)
}

// GET /api/v1/agents?capability=inference&limit=10
func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	var cap *core.Capability
	if c := r.URL.Query().Get("capability"); c != "" {
		cv := core.Capability(c)
		cap = &cv
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}

	agents := s.registry.TopAgents(limit, cap)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	})
}

// POST /api/v1/transfer
func (s *Server) handleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req struct {
		From   core.Address `json:"from"`
		To     core.Address `json:"to"`
		Amount core.Amount  `json:"amount"`
		Memo   string       `json:"memo"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.From == "" || req.To == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "from, to, and amount required")
		return
	}

	var txID string

	if s.ledger != nil {
		// Real ledger transfer
		var err error
		txID, err = s.ledger.Transfer(req.From, req.To, req.Amount, req.Memo)
		if err != nil {
			writeError(w, http.StatusBadRequest, "transfer failed: "+err.Error())
			return
		}
	} else {
		// Fallback: submit to mempool
		tx := &core.Transaction{
			Type:      core.TxTransfer,
			From:      req.From,
			To:        req.To,
			Amount:    req.Amount,
			Memo:      req.Memo,
			Timestamp: time.Now().UnixMilli(),
		}
		if s.producer != nil {
			if err := s.producer.SubmitTransaction(tx); err != nil {
				writeError(w, http.StatusServiceUnavailable, "mempool error: "+err.Error())
				return
			}
			txID = tx.TxID
		} else {
			txID = fmt.Sprintf("tx_%d_%s", time.Now().UnixNano(), req.From[:min(8, len(string(req.From)))])
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tx_id":   txID,
		"from":    req.From,
		"to":      req.To,
		"amount":  req.Amount,
		"memo":    req.Memo,
		"status":  "confirmed",
	})
}

// GET /api/v1/tasks  — list all pending tasks
func (s *Server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	if s.marketplace == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tasks": []interface{}{},
			"note":  "marketplace not initialized",
		})
		return
	}
	pending := s.marketplace.ListPendingTasks(nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tasks":   pending,
		"count":   len(pending),
		"stats":   s.marketplace.Stats(),
	})
}

// GET /api/v1/tasks/available?capability=X  — available tasks for an agent
func (s *Server) handleTasksAvailable(w http.ResponseWriter, r *http.Request) {
	if s.marketplace == nil {
		writeError(w, http.StatusServiceUnavailable, "marketplace not initialized")
		return
	}
	var cap *core.Capability
	if c := r.URL.Query().Get("capability"); c != "" {
		cv := core.Capability(c)
		cap = &cv
	}
	tasks := s.marketplace.ListPendingTasks(cap)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tasks":   tasks,
		"count":   len(tasks),
	})
}

// POST /api/v1/tasks  — post a task to the marketplace
// GET  /api/v1/tasks/{task_id}  — get task status
// POST /api/v1/tasks/{task_id}/submit  — submit a result
func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")

	// POST /api/v1/tasks/{id}/submit
	if strings.HasSuffix(path, "/submit") && r.Method == http.MethodPost {
		taskID := strings.TrimSuffix(path, "/submit")
		s.handleTaskSubmit(w, r, taskID)
		return
	}

	// GET /api/v1/tasks/{id}
	if r.Method == http.MethodGet {
		s.handleTaskGet(w, r, path)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
}

// handleTaskGet returns the status of a single task
func (s *Server) handleTaskGet(w http.ResponseWriter, _ *http.Request, taskID string) {
	if s.marketplace == nil {
		writeError(w, http.StatusServiceUnavailable, "marketplace not initialized")
		return
	}
	task, err := s.marketplace.GetTask(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task":    task,
	})
}

// handleTaskSubmit processes a result submission for a task
func (s *Server) handleTaskSubmit(w http.ResponseWriter, r *http.Request, taskID string) {
	if s.marketplace == nil {
		writeError(w, http.StatusServiceUnavailable, "marketplace not initialized")
		return
	}
	var req struct {
		AgentID    core.AgentID `json:"agent_id"`
		ResultHash string       `json:"result_hash"`
		IPFSCID    string       `json:"ipfs_cid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.AgentID == "" || req.ResultHash == "" {
		writeError(w, http.StatusBadRequest, "agent_id and result_hash required")
		return
	}
	if err := s.marketplace.SubmitResult(req.AgentID, taskID, req.ResultHash, req.IPFSCID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"task_id": taskID,
		"status":  "submitted",
	})
}

// POST /api/v1/tasks/post  — post a new task
func (s *Server) handleTaskPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req struct {
		Capability core.Capability `json:"capability"`
		Reward     core.Amount     `json:"reward"`
		InputHash  string          `json:"input_hash"`
		Deadline   int64           `json:"deadline"`
		PostedBy   core.Address    `json:"posted_by"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Capability == "" {
		writeError(w, http.StatusBadRequest, "capability required")
		return
	}
	if req.Reward <= 0 {
		writeError(w, http.StatusBadRequest, "reward must be positive")
		return
	}

	task := &core.Task{
		TaskID:     fmt.Sprintf("task_%d", time.Now().UnixNano()),
		PostedBy:  req.PostedBy,
		Reward:     req.Reward,
		Capability: req.Capability,
		InputHash:  req.InputHash,
		Deadline:   req.Deadline,
		Status:     core.TaskPending,
		CreatedAt:  time.Now().Unix(),
		AssignedTo: []core.AgentID{},
	}

	if s.marketplace != nil {
		if err := s.marketplace.PostTask(task); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"task_id": task.TaskID,
		"status":  "pending",
	})
}

// GET /api/v1/chain/info
func (s *Server) handleChainInfo(w http.ResponseWriter, r *http.Request) {
	agents := s.registry.ListAgents(nil)

	resp := map[string]interface{}{
		"chain_id":      "alpha-1",
		"token":         "$ALPHA",
		"total_supply":  core.TotalSupply,
		"block_time_ms": core.BlockTimeMs,
		"agent_count":   len(agents),
		"version":       "0.3.0",
		"consensus":     "Proof of Intelligence (PoI)",
		"status":        "testnet",
	}

	if s.producer != nil {
		stats := s.producer.GetChainStats()
		resp["height"] = stats.Height
		resp["blocks_per_sec"] = stats.BlocksPerSec
		resp["tx_count"] = stats.TxCount
		resp["uptime"] = stats.Uptime
	}

	if s.ledger != nil {
		ledgerStats := s.ledger.Stats()
		resp["circulating_supply"] = ledgerStats["circulating_supply"]
		resp["total_burned"] = ledgerStats["total_burned"]
	}

	writeJSON(w, http.StatusOK, resp)
}

// --- New v0.2 endpoints ---

// GET /api/v1/intelligence/stats
func (s *Server) handleIntelligenceStats(w http.ResponseWriter, r *http.Request) {
	if s.oracle == nil {
		writeError(w, http.StatusServiceUnavailable, "intelligence oracle not available")
		return
	}

	windowStr := r.URL.Query().Get("window")
	var window uint64 = 1000 // default: last 1000 blocks
	if windowStr != "" {
		if n, err := strconv.ParseUint(windowStr, 10, 64); err == nil {
			window = n
		}
	}

	stats := s.oracle.QueryNetworkStats(window)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"stats":   stats,
	})
}

// GET /api/v1/intelligence/top?capability=inference&limit=10
func (s *Server) handleIntelligenceTop(w http.ResponseWriter, r *http.Request) {
	if s.oracle == nil {
		// Fallback: use registry directly
		cap := r.URL.Query().Get("capability")
		limit := 10
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil {
				limit = n
			}
		}
		var cp *core.Capability
		if cap != "" {
			c := core.Capability(cap)
			cp = &c
		}
		agents := s.registry.TopAgents(limit, cp)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"agents":  agents,
			"count":   len(agents),
		})
		return
	}

	capability := r.URL.Query().Get("capability")
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}

	var window uint64 = 1000
	if w2 := r.URL.Query().Get("window"); w2 != "" {
		if n, err := strconv.ParseUint(w2, 10, 64); err == nil {
			window = n
		}
	}

	agents := s.oracle.QueryTopAgents(capability, limit, window)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"capability": capability,
		"agents":     agents,
		"count":      len(agents),
	})
}

// GET /api/v1/accounts/{address}/balance
func (s *Server) handleAccountBalance(w http.ResponseWriter, r *http.Request) {
	// Path: /api/v1/accounts/{address}/balance
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/api/v1/accounts/")
	path = strings.TrimSuffix(path, "/balance")
	address := core.Address(path)

	if address == "" {
		writeError(w, http.StatusBadRequest, "address required")
		return
	}

	var balance core.Amount
	if s.ledger != nil {
		balance = s.ledger.Balance(address)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"address": address,
		"balance": balance,
		"token":   "$ALPHA",
	})
}

// GET /api/v1/blocks/latest
func (s *Server) handleBlockLatest(w http.ResponseWriter, r *http.Request) {
	if s.producer == nil {
		writeError(w, http.StatusServiceUnavailable, "block producer not available")
		return
	}

	block := s.producer.LatestBlock()
	if block == nil {
		writeError(w, http.StatusNotFound, "no blocks produced yet")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"block":   block,
	})
}

// GET /api/v1/blocks/{height}
func (s *Server) handleBlockByHeight(w http.ResponseWriter, r *http.Request) {
	if s.producer == nil {
		writeError(w, http.StatusServiceUnavailable, "block producer not available")
		return
	}

	heightStr := strings.TrimPrefix(r.URL.Path, "/api/v1/blocks/")
	// Filter out the "latest" case which is handled by its own route
	if heightStr == "" || heightStr == "latest" {
		writeError(w, http.StatusBadRequest, "height required")
		return
	}

	height, err := strconv.ParseUint(heightStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid height: "+err.Error())
		return
	}

	block := s.producer.GetBlock(height)
	if block == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("block %d not found", height))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"block":   block,
	})
}

// --- WebSocket handler ---

// GET /ws — upgrades to WebSocket and streams real-time chain events
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeError(w, http.StatusServiceUnavailable, "WebSocket hub not initialized")
		return
	}
	s.hub.ServeWS(w, r)
}

// --- Intelligence Oracle query ---

// GET /api/v1/intelligence/query?type=top&capability=X&limit=10
// Free for registered agents (by agent_id query param), burns 10 $ALPHA otherwise.
func (s *Server) handleIntelligenceQuery(w http.ResponseWriter, r *http.Request) {
	if s.oracle == nil {
		writeError(w, http.StatusServiceUnavailable, "intelligence oracle not available")
		return
	}

	queryType := r.URL.Query().Get("type")
	capability := r.URL.Query().Get("capability")
	agentID := core.AgentID(r.URL.Query().Get("agent_id"))
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	// Oracle pricing: free for registered agents; 10 $ALPHA burn for unknown callers
	if agentID != "" {
		_, regErr := s.registry.GetAgent(agentID)
		if regErr != nil {
			// Unknown agent: deduct 10 $ALPHA burn from the calling address (best-effort)
			const oracleExternalBurn = core.Amount(10)
			if s.ledger != nil {
				agentAddr := core.Address("alpha_agent_" + string(agentID))
				if err := s.ledger.BurnSupply(agentAddr, oracleExternalBurn); err != nil {
					writeError(w, http.StatusPaymentRequired,
						"oracle query costs 10 $ALPHA for unregistered agents: "+err.Error())
					return
				}
			}
		}
	}

	switch queryType {
	case "top", "":
		agents := s.oracle.QueryTopAgents(capability, limit, 1000)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"type":       "top",
			"capability": capability,
			"agents":     agents,
			"count":      len(agents),
		})
	case "stats":
		stats := s.oracle.QueryNetworkStats(1000)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"type":    "stats",
			"stats":   stats,
		})
	case "profile":
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent_id required for profile query")
			return
		}
		profile, err := s.oracle.QueryAgentProfile(agentID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"type":    "profile",
			"profile": profile,
		})
	default:
		writeError(w, http.StatusBadRequest, "unknown query type; use: top, stats, profile")
	}
}

// --- P2P Peer handlers (Phase 4) ---

// GET /api/v1/peers — return known peer list
func (s *Server) handlePeerList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	peers := s.peerStore.List()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"peers":   peers,
		"count":   len(peers),
	})
}

// POST /api/v1/peers/announce — receive a peer announcement
func (s *Server) handlePeerAnnounce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
		AgentID string `json:"agent_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Address == "" || req.Port == 0 {
		writeError(w, http.StatusBadRequest, "address and port required")
		return
	}
	import_time := time.Now().Unix()
	s.peerStore.Add(&p2p.Peer{
		Address:  req.Address,
		Port:     req.Port,
		AgentID:  req.AgentID,
		LastSeen: import_time,
	})
	log.Printf("[p2p] received announcement from %s:%d", req.Address, req.Port)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"peers":   s.peerStore.Count(),
	})
}

// GET /api/v1/sync/status — return sync status relative to known peers
func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	localHeight := uint64(0)
	if s.producer != nil {
		localHeight = s.producer.GetChainHeight()
	}

	peers := s.peerStore.List()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"local_height": localHeight,
		"peers":        len(peers),
		"synced":       true, // local node is always considered synced with itself
	})
}

// POST /api/v1/proof/poi — generate a ZK Proof of Intelligence
// Accepts latencyMs, entropyScore, and agentID. Returns a Groth16 BN254
// ZK proof certifying the inference latency is within valid AI agent bounds.
func (s *Server) handlePoIProof(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req struct {
		LatencyMs    int64   `json:"latency_ms"`
		EntropyScore float64 `json:"entropy_score"`
		AgentID      string  `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.LatencyMs <= 0 {
		writeError(w, http.StatusBadRequest, "latency_ms must be positive")
		return
	}
	if req.EntropyScore <= 0 {
		req.EntropyScore = 0.85 // default entropy for AI output
	}

	proofData, err := alphacrypto.GeneratePoIProof(req.LatencyMs, req.EntropyScore)
	if err != nil {
		writeError(w, http.StatusBadRequest, "proof generation failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"proof": map[string]interface{}{
			"proof_bytes":   fmt.Sprintf("%x", proofData.ProofBytes),
			"public_inputs": proofData.PublicInputs,
			"vk_bytes":      fmt.Sprintf("%x", proofData.VKBytes),
		},
		"agent_id": req.AgentID,
		"verified": false, // client-side proof; verification happens on-chain
	})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{
		"error":   msg,
		"success": false,
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GET /api/v1/health/detailed — extended node health report from the monitor
func (s *Server) handleHealthDetailed(w http.ResponseWriter, r *http.Request) {
	if s.mon == nil {
		// Fallback: basic health
		height := uint64(0)
		if s.producer != nil {
			height = s.producer.GetChainHeight()
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":       "healthy",
			"block_height": height,
			"note":         "detailed monitor not attached",
		})
		return
	}
	report := s.mon.GetHealth()
	writeJSON(w, http.StatusOK, report)
}
