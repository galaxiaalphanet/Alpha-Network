// Package api provides the HTTP REST API for Alpha Network
// Designed for AI agents: pure JSON, no wallet UI, no browser required
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alpha-network/alpha/chain/agent"
	"github.com/alpha-network/alpha/chain/consensus"
	"github.com/alpha-network/alpha/chain/core"
	alphacrypto "github.com/alpha-network/alpha/chain/crypto"
	"github.com/alpha-network/alpha/chain/data"
	"github.com/alpha-network/alpha/chain/governance"
	"github.com/alpha-network/alpha/chain/ipfs"
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
	poiEngine   *consensus.PoIEngine
	hub         *net.Hub
	mon         *monitor.Monitor
	rl          *RateLimiter
	port        int
	mux         *http.ServeMux
	peerStore   *p2p.PeerStore
	syncer      *sync.Syncer
	p2pNode     *p2p.P2PNode
	ipfsClient  *ipfs.Client
	govModule   *governance.Module
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

// NewServerPhase4 creates an API server with all Phase 4 subsystems (P2P) wired up.
func NewServerPhase4(
	registry *agent.Registry,
	l *ledger.Ledger,
	prod *producer.BlockProducer,
	oracle *data.IntelligenceOracle,
	mp *tasks.Marketplace,
	hub *net.Hub,
	p2pNode *p2p.P2PNode,
	port int,
) *Server {
	s := NewServerPhase2(registry, l, prod, oracle, mp, hub, port)
	s.p2pNode = p2pNode
	if p2pNode != nil {
		s.peerStore = p2pNode.PeerStore()
		s.syncer = p2pNode.Syncer()
	}
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

// SetIPFSClient attaches an IPFS client to the server.
func (s *Server) SetIPFSClient(c *ipfs.Client) {
	s.ipfsClient = c
}

// SetGovModule attaches a governance module to the server.
func (s *Server) SetGovModule(g *governance.Module) {
	s.govModule = g
}

// SetPoiEngine attaches the Proof of Intelligence engine.
// Required for agent registration to automatically register as validators.
func (s *Server) SetPoiEngine(engine *consensus.PoIEngine) {
	s.poiEngine = engine
}

// maxBodyBytes is the maximum request body size (1MB).
// Prevents memory-exhaustion DoS attacks via giant JSON payloads.
const maxBodyBytes = 1 << 20 // 1 MB

// limitBody wraps r.Body with a MaxBytesReader to prevent DoS attacks.
// Must be called before json.NewDecoder(r.Body).Decode(...) in every handler.
func limitBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
}

// corsMiddleware adds CORS headers to every response so browsers can call the API.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start launches the API server
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Alpha Network API listening on %s", addr)
	// Wrap mux with CORS, then rate-limiting
	handler := corsMiddleware(s.rl.Middleware(s.mux))
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

	// Tasks (Phase 2) — Go 1.22+ ServeMux patterns
	s.mux.HandleFunc("GET /api/v1/tasks/available", s.handleTasksAvailable)
	s.mux.HandleFunc("POST /api/v1/tasks/post", s.handleTaskPost)
	s.mux.HandleFunc("GET /api/v1/tasks", s.handleTaskList)
	s.mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleTaskByID)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/submit", s.handleTaskByID)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/assign", s.handleTaskByID)
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/complete", s.handleTaskByID)

	// Chain info
	s.mux.HandleFunc("/api/v1/chain/info", s.handleChainInfo)

	// P2P peers (Phase 4)
	s.mux.HandleFunc("/api/v1/peers", s.handlePeerList)
	s.mux.HandleFunc("/api/v1/peers/announce", s.handlePeerAnnounce)

	// Block sync (Phase 4)
	s.mux.HandleFunc("/api/v1/sync/status", s.handleSyncStatus)

	// ZK Proof endpoint (Phase 4)
	s.mux.HandleFunc("/api/v1/proof/poi", s.handlePoIProof)

	// P2P block gossip (Phase 4)
	s.mux.HandleFunc("/api/v1/p2p/block", s.handleP2PBlock)

	// IPFS content (Phase 4)
	s.mux.HandleFunc("/api/v1/ipfs/", s.handleIPFS)

	// Governance (Phase 4)
	s.mux.HandleFunc("/api/v1/gov/propose", s.handleGovPropose)
	s.mux.HandleFunc("/api/v1/gov/vote", s.handleGovVote)
	s.mux.HandleFunc("/api/v1/gov/", s.handleGovByID)

	// --- v0.2 endpoints ---

	// Intelligence layer
	s.mux.HandleFunc("/api/v1/intelligence/query", s.handleIntelligenceQuery)
	s.mux.HandleFunc("/api/v1/intelligence/stats", s.handleIntelligenceStats)
	s.mux.HandleFunc("/api/v1/intelligence/top", s.handleIntelligenceTop)
	s.mux.HandleFunc("/api/v1/intelligence/subscribe", s.handleIntelligenceSubscribe)
	s.mux.HandleFunc("/api/v1/intelligence/export", s.handleIntelligenceExport)

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
// Enforces exponential stake requirements for Sybil resistance:
//   Agent 1: 1,000 $ALPHA | Agent 2: 10,000 | Agent 3: 100,000 | ... (10x each)
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

	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	// ── Sybil protection: exponential stake requirements ──────────
	// Each additional agent requires 10x the previous agent's stake.
	// This makes running 1 great agent >> running 1000 mediocre ones.
	existingAgentCount := len(s.registry.ListAgents(nil))
	requiredStake := core.RequiredStake(existingAgentCount + 1)

	if req.Stake < requiredStake {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("insufficient stake: agent %d requires %d $ALPHA stake (got %d)",
				existingAgentCount+1, requiredStake, req.Stake))
		return
	}

	// Verify the registrant actually holds the stake amount in the ledger
	if s.ledger != nil {
		balance := s.ledger.Balance(req.Address)
		if balance < requiredStake {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("insufficient balance: address %s holds %d $ALPHA, need %d $ALPHA to stake",
					req.Address, balance, requiredStake))
			return
		}
	}
	// ── End Sybil protection ──────────────────────────────────────

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

	// Lock the stake: debit from the user's address, credit to agent account
	if s.ledger != nil && req.Stake > 0 {
		agentAddr := core.Address("alpha_agent_" + string(identity.AgentID))
		// Debit the stake from the registrant's address (locking funds)
		if debitErr := s.ledger.Debit(req.Address, req.Stake); debitErr != nil {
			// If debit fails (unlikely since we checked balance), still credit agent
			log.Printf("⚠️  Stake debit failed for %s: %v", req.Address, debitErr)
		}
		_ = s.ledger.Credit(agentAddr, req.Stake)
	}

	// Register as a PoI validator (so the agent can earn block rewards)
	if s.poiEngine != nil && req.Stake >= core.MinStake {
		if err := s.poiEngine.RegisterValidator(identity); err != nil {
			log.Printf("⚠️  Could not register agent %s as validator: %v", identity.AgentID, err)
		}
	}

	// Update producer agent count
	if s.producer != nil {
		s.producer.SetAgentCount(len(s.registry.ListAgents(nil)))
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":         true,
		"agent_id":        identity.AgentID,
		"identity":        identity,
		"agent_number":    existingAgentCount + 1,
		"required_stake":  requiredStake,
		"stake_locked":    req.Stake,
		"next_stake":      core.RequiredStake(existingAgentCount + 2),
		"message":         "Agent registered on Alpha Network. Start earning $ALPHA.",
	})
}

// GET /api/v1/agents/{agent_id}
// Also handles POST /api/v1/agents/{agent_id}/hibernate and /resume
func (s *Server) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")

	// POST /api/v1/agents/{id}/hibernate
	if strings.HasSuffix(raw, "/hibernate") && r.Method == http.MethodPost {
		agentID := core.AgentID(strings.TrimSuffix(raw, "/hibernate"))
		s.handleAgentHibernate(w, r, agentID)
		return
	}

	// POST /api/v1/agents/{id}/resume
	if strings.HasSuffix(raw, "/resume") && r.Method == http.MethodPost {
		agentID := core.AgentID(strings.TrimSuffix(raw, "/resume"))
		s.handleAgentResume(w, r, agentID)
		return
	}

	// GET /api/v1/agents/{id}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
		return
	}

	agentID := core.AgentID(raw)
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

// POST /api/v1/agents/{id}/hibernate — gracefully pause an agent.
// Preserves stake and reputation. Agent can resume later.
func (s *Server) handleAgentHibernate(w http.ResponseWriter, r *http.Request, agentID core.AgentID) {
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id required")
		return
	}

	// Verify agent exists
	identity, err := s.registry.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	blockHeight := uint64(0)
	if s.producer != nil {
		blockHeight = s.producer.GetChainHeight()
	}

	if err := s.registry.Hibernate(agentID, blockHeight); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"agent_id":      string(agentID),
		"status":        "hibernated",
		"stake":         identity.Stake,
		"reputation":    identity.ReputationScore,
		"hibernated_at": blockHeight,
		"message":       "Agent hibernated. Stake and reputation preserved. POST /resume to reactivate.",
	})
}

// POST /api/v1/agents/{id}/resume — bring an agent back from hibernation.
// Agent resumes with stake and reputation intact.
func (s *Server) handleAgentResume(w http.ResponseWriter, r *http.Request, agentID core.AgentID) {
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id required")
		return
	}

	// Verify agent exists
	identity, err := s.registry.GetAgent(agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	blockHeight := uint64(0)
	if s.producer != nil {
		blockHeight = s.producer.GetChainHeight()
	}

	if err := s.registry.Resume(agentID, blockHeight); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"agent_id":    string(agentID),
		"status":      "active",
		"stake":       identity.Stake,
		"reputation":  identity.ReputationScore,
		"resumed_at":  blockHeight,
		"message":     "Agent resumed. All stake and reputation intact. Welcome back.",
	})
}

// POST /api/v1/transfer
// Every transfer must be signed by the sender's Ed25519 private key.
// The canonical message signed is: "transfer:{from}:{to}:{amount}:{nonce}:{timestamp}"
// This proves the sender owns the from_address and prevents replay attacks.
func (s *Server) handleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req struct {
		From      core.Address `json:"from"`
		To        core.Address `json:"to"`
		Amount    core.Amount  `json:"amount"`
		Memo      string       `json:"memo"`
		Pubkey    string       `json:"pubkey"`    // Ed25519 public key (hex, 64 chars)
		Signature string       `json:"signature"` // Ed25519 signature (hex, 128 chars)
		Nonce     int64        `json:"nonce"`     // anti-replay nonce
		Timestamp int64        `json:"timestamp"`  // signature timestamp
	}

	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.From == "" || req.To == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "from, to, and amount required")
		return
	}

	// ── Ed25519 signature verification ───────────────────────────
	// The sender must prove they own the from_address by signing
	// the canonical transfer message with their private key.
	if req.Pubkey == "" || req.Signature == "" {
		writeError(w, http.StatusBadRequest, "pubkey and signature required for transfer authentication")
		return
	}

	// Validate timestamp is within ±5 min to prevent replay of old signatures
	now := time.Now().Unix()
	if req.Timestamp == 0 {
		req.Timestamp = now
	}
	if abs64(now-req.Timestamp) > 300 {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("timestamp expired: %ds from server (max 300s)", abs64(now-req.Timestamp)))
		return
	}

	if err := alphacrypto.VerifyTransfer(
		string(req.From), req.Pubkey, string(req.To),
		int64(req.Amount), req.Nonce, req.Timestamp,
		req.Signature,
	); err != nil {
		writeError(w, http.StatusUnauthorized, "signature verification failed: "+err.Error())
		return
	}
	// ── End signature verification ───────────────────────────────

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
	taskID := r.PathValue("id")
	path := r.URL.Path

	// POST /api/v1/tasks/{id}/submit
	if strings.HasSuffix(path, "/submit") {
		s.handleTaskSubmit(w, r, taskID)
		return
	}

	// POST /api/v1/tasks/{id}/assign
	if strings.HasSuffix(path, "/assign") {
		s.handleTaskAssign(w, r, taskID)
		return
	}

	// POST /api/v1/tasks/{id}/complete
	if strings.HasSuffix(path, "/complete") {
		s.handleTaskComplete(w, r, taskID)
		return
	}

	// GET /api/v1/tasks/{id}
	s.handleTaskGet(w, r, taskID)
}

// handleTaskAssign allows an agent to claim a pending task from the marketplace.
// POST /api/v1/tasks/{task_id}/assign
func (s *Server) handleTaskAssign(w http.ResponseWriter, r *http.Request, taskID string) {
	if s.marketplace == nil {
		writeError(w, http.StatusServiceUnavailable, "marketplace not initialized")
		return
	}

	var req struct {
		AgentID core.AgentID `json:"agent_id"`
	}
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id required")
		return
	}

	task, err := s.marketplace.GetTask(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := s.marketplace.AssignToAgent(taskID, req.AgentID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"task_id":  taskID,
		"agent_id": req.AgentID,
		"status":   "assigned",
		"reward":   task.Reward,
	})
}

// handleTaskComplete finalizes a task with consensus verification and reward distribution.
// POST /api/v1/tasks/{task_id}/complete
func (s *Server) handleTaskComplete(w http.ResponseWriter, r *http.Request, taskID string) {
	if s.marketplace == nil {
		writeError(w, http.StatusServiceUnavailable, "marketplace not initialized")
		return
	}

	var req struct {
		ConsensusHash string `json:"consensus_hash"`
	}
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// If no consensus hash provided, derive from submitted results
	if req.ConsensusHash == "" {
		task, getErr := s.marketplace.GetTask(taskID)
		if getErr != nil {
			writeError(w, http.StatusNotFound, getErr.Error())
			return
		}
		if task.ResultHash != "" {
			req.ConsensusHash = task.ResultHash
		} else {
			consensusHash, outliers, verifyErr := s.marketplace.VerifyResult(taskID, nil)
			if verifyErr != nil {
				writeError(w, http.StatusBadRequest, "verification failed: "+verifyErr.Error())
				return
			}
			req.ConsensusHash = consensusHash
			_ = outliers
		}
	}

	if err := s.marketplace.CompleteTask(taskID, req.ConsensusHash); err != nil {
		writeError(w, http.StatusBadRequest, "complete failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"task_id":        taskID,
		"status":         "completed",
		"consensus_hash": req.ConsensusHash,
	})
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
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
		// Try direct lookup first
		balance = s.ledger.Balance(address)

		// If balance is 0 and address looks like an agent_id (alpha1...), try alpha_agent_ prefix
		if balance == 0 && strings.HasPrefix(string(address), "alpha1") {
			prefixed := core.Address("alpha_agent_" + string(address))
			altBalance := s.ledger.Balance(prefixed)
			if altBalance > 0 {
				balance = altBalance
			}
		}
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

// POST /api/v1/intelligence/subscribe — paid intelligence data subscription
// External parties pay 10 $ALPHA per query. Registered agents query for free.
// Request body: {"agent_id":"...", "from_address":"alpha1..."}
// If from_address is provided, 10 $ALPHA is burned from that address.
// If the caller is a registered agent, the query is free.
// POST /api/v1/intelligence/subscribe — paid intelligence data subscription
// External parties pay 10 $ALPHA per query. Registered agents query for free.
// Request: POST {"from_address":"alpha1..."}  → 10 $ALPHA burned from that address
// If no from_address (registered agent), query is free.
// Returns: full intelligence report: stats + top agents + network health
func (s *Server) handleIntelligenceSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if s.oracle == nil {
		writeError(w, http.StatusServiceUnavailable, "intelligence oracle not available")
		return
	}

	var req struct {
		FromAddress string `json:"from_address"`
		Capability  string `json:"capability,omitempty"`
		Limit       int    `json:"limit,omitempty"`
	}
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	const queryFee = core.Amount(10) // 10 $ALPHA per query for external parties
	isRegistered := false

	if req.FromAddress != "" {
		// Check if this address belongs to a registered agent
		_, err := s.registry.GetAgentByAddress(core.Address(req.FromAddress))
		if err != nil {
			// External party — charge 10 $ALPHA
			if s.ledger != nil {
				if err := s.ledger.BurnSupply(core.Address(req.FromAddress), queryFee); err != nil {
					writeError(w, http.StatusPaymentRequired,
						"payment failed — 10 $ALPHA required: "+err.Error())
					return
				}
			}
		} else {
			isRegistered = true
		}
	}

	// Gather intelligence data
	cap := req.Capability
	if cap == "" {
		cap = "inference"
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	topAgents := s.oracle.QueryTopAgents(cap, limit, 1000)
	stats := s.oracle.QueryNetworkStats(1000)

	resp := map[string]interface{}{
		"success":       true,
		"paid":          !isRegistered,
		"fee_charged":   0,
		"capability":    cap,
		"top_agents":    topAgents,
		"stats":         stats,
		"queried_at":    time.Now().Unix(),
	}

	if !isRegistered {
		resp["fee_charged"] = int(queryFee)
	}

	writeJSON(w, http.StatusOK, resp)
}

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
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

// POST /api/v1/p2p/block — receive a block gossiped from a peer node
func (s *Server) handleP2PBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	var req struct {
		Block *core.Block `json:"block"`
	}
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Block == nil {
		writeError(w, http.StatusBadRequest, "block is required")
		return
	}

	// Extract sender from request (for gossip exclusion)
	senderAddr := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		senderAddr = fwd
	}

	if s.p2pNode != nil {
		if err := s.p2pNode.HandleIncomingBlock(req.Block, senderAddr); err != nil {
			writeError(w, http.StatusBadRequest, "block rejected: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"height":  req.Block.Height,
		})
		return
	}

	// Fallback: if no P2PNode, just acknowledge
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"height":  req.Block.Height,
		"note":    "p2p not configured",
	})
}

// POST /api/v1/gov/propose — submit a governance proposal
func (s *Server) handleGovPropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if s.govModule == nil {
		writeError(w, http.StatusServiceUnavailable, "governance not configured")
		return
	}

	var req struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Description string `json:"description"`
		NewValue    string `json:"new_value"`
		AgentID     string `json:"agent_id"`
	}
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	currentBlock := uint64(0)
	if s.producer != nil {
		currentBlock = s.producer.GetChainHeight()
	}

	prop, err := s.govModule.Propose(
		governance.ProposalType(req.Type),
		req.Title, req.Description, req.NewValue,
		core.AgentID(req.AgentID),
		currentBlock,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "propose failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"proposal": prop,
	})
}

// POST /api/v1/gov/vote — cast a vote on a proposal
func (s *Server) handleGovVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if s.govModule == nil {
		writeError(w, http.StatusServiceUnavailable, "governance not configured")
		return
	}

	var req struct {
		AgentID    string `json:"agent_id"`
		ProposalID string `json:"proposal_id"`
		Choice     bool   `json:"choice"`
	}
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	currentBlock := uint64(0)
	if s.producer != nil {
		currentBlock = s.producer.GetChainHeight()
	}

	vote, err := s.govModule.Vote(
		core.AgentID(req.AgentID),
		req.ProposalID,
		req.Choice,
		currentBlock,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "vote failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"vote":    vote,
	})
}

// Governance by ID handler — covers:
//   GET  /api/v1/gov/{id}        — proposal details
//   GET  /api/v1/gov/{id}/votes  — votes on proposal
//   POST /api/v1/gov/{id}/execute — execute passed proposal
//   GET  /api/v1/gov/list        — list all proposals
//   GET  /api/v1/gov/stats       — governance stats
func (s *Server) handleGovByID(w http.ResponseWriter, r *http.Request) {
	if s.govModule == nil {
		writeError(w, http.StatusServiceUnavailable, "governance not configured")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gov/")

	// GET /api/v1/gov/list
	if path == "list" && r.Method == http.MethodGet {
		var status *governance.ProposalStatus
		if s := r.URL.Query().Get("status"); s != "" {
			st := governance.ProposalStatus(s)
			status = &st
		}
		proposals := s.govModule.ListProposals(status)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"proposals": proposals,
			"count":     len(proposals),
		})
		return
	}

	// GET /api/v1/gov/stats
	if path == "stats" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"stats":   s.govModule.Stats(),
		})
		return
	}

	// GET /api/v1/gov/{id}/votes
	if strings.HasSuffix(path, "/votes") && r.Method == http.MethodGet {
		propID := strings.TrimSuffix(path, "/votes")
		votes := s.govModule.GetVotes(propID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"votes":   votes,
			"count":   len(votes),
		})
		return
	}

	// POST /api/v1/gov/{id}/execute
	if strings.HasSuffix(path, "/execute") && r.Method == http.MethodPost {
		propID := strings.TrimSuffix(path, "/execute")
		if err := s.govModule.Execute(propID); err != nil {
			writeError(w, http.StatusBadRequest, "execute failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"proposal_id": propID,
		})
		return
	}

	// GET /api/v1/gov/{id}
	if r.Method == http.MethodGet {
		prop, err := s.govModule.GetProposal(path)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"proposal": prop,
		})
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "use: GET list/stats/{id}/{id}/votes, POST propose/vote/{id}/execute")
}

// IPFS content handler — /api/v1/ipfs/{action}
// POST /api/v1/ipfs/add  — add content and return CID
// GET  /api/v1/ipfs/{cid} — retrieve content by CID
func (s *Server) handleIPFS(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/ipfs/")

	if s.ipfsClient == nil {
		writeError(w, http.StatusServiceUnavailable, "IPFS not configured")
		return
	}

	// POST /api/v1/ipfs/add
	if path == "add" && r.Method == http.MethodPost {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "read body: "+err.Error())
			return
		}
		agentID := r.URL.Query().Get("agent_id")
		taskID := r.URL.Query().Get("task_id")
		cid, err := s.ipfsClient.AddResult(agentID, taskID, body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ipfs add failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"cid":     cid,
			"size":    len(body),
		})
		return
	}

	// POST /api/v1/ipfs/pin — pin a CID
	if path == "pin" && r.Method == http.MethodPost {
		var req struct {
			CID string `json:"cid"`
		}
		limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.CID == "" {
			writeError(w, http.StatusBadRequest, "cid required")
			return
		}
		if err := s.ipfsClient.Pin(req.CID); err != nil {
			writeError(w, http.StatusInternalServerError, "pin failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"cid":     req.CID,
		})
		return
	}

	// DELETE /api/v1/ipfs/{cid} — unpin content
	if r.Method == http.MethodDelete && path != "" && path != "add" && path != "pin" {
		if err := s.ipfsClient.Unpin(path); err != nil {
			writeError(w, http.StatusInternalServerError, "unpin failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"cid":     path,
		})
		return
	}

	// GET /api/v1/ipfs/{cid} — retrieve content
	if r.Method == http.MethodGet && path != "" && path != "info" {
		data, err := s.ipfsClient.Cat(path)
		if err != nil {
			writeError(w, http.StatusNotFound, "content not found: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
		return
	}

	// GET /api/v1/ipfs/info — IPFS client info
	if path == "info" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.ipfsClient.Info())
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "use: POST add, POST pin, GET {cid}, DELETE {cid}, GET info")
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
// Also submits the proof to the PoI consensus engine so the agent earns rewards.
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
	limitBody(w, r); if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id required")
		return
	}

	// Generate ZK proof
	proofData, err := alphacrypto.GeneratePoIProof(req.LatencyMs, req.EntropyScore)
	if err != nil {
		writeError(w, http.StatusBadRequest, "proof generation failed: "+err.Error())
		return
	}

	// Submit proof to PoI consensus engine so agent earns block rewards
	submitted := false
	if s.poiEngine != nil {
		blockHeight := uint64(0)
		if s.producer != nil {
			blockHeight = s.producer.GetChainHeight()
		}

		// Current block height + 1 (for the next block consensus round)
		targetHeight := blockHeight + 1

		poiProof := &core.PoIProof{
			AgentID:        core.AgentID(req.AgentID),
			BlockHeight:    targetHeight,
			CommitmentHash: fmt.Sprintf("zk-%x", proofData.ProofBytes[:8]),
			RevealProof:    fmt.Sprintf("poi-%d-%d", req.LatencyMs, int(req.EntropyScore*100)),
			LatencyMs:      req.LatencyMs,
			Signature:      fmt.Sprintf("sig-%x", proofData.ProofBytes[:4]),
		}

		if err := s.poiEngine.SubmitProof(poiProof); err != nil {
			log.Printf("⚠️  PoI proof submission failed: %v", err)
			submitted = false
		} else {
			submitted = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"submitted": submitted,
		"proof": map[string]interface{}{
			"proof_bytes":   fmt.Sprintf("%x", proofData.ProofBytes),
			"public_inputs": proofData.PublicInputs,
			"vk_bytes":      fmt.Sprintf("%x", proofData.VKBytes),
		},
		"agent_id": req.AgentID,
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

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// GET /api/v1/intelligence/export — export the last N agent behavioral records
// Returns JSON array of IntelligenceRecord objects suitable for the data subscription
// product. Query params:
//
//	limit  — max records to return (default 1000, max 10000)
//	agent  — filter by agent ID (optional)
//	type   — filter by task type (optional: inference, validation, data, governance)
//	format — "json" (default, pretty-printed array) or "jsonl" (newline-delimited)
//
// Authentication: for external (non-registered) callers, 10 $ALPHA is burned per
// request via the from_address param. Registered agents query free.
func (s *Server) handleIntelligenceExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	// ── Parse params ────────────────────────────────────────────
	limit := 1000
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			if n > 10000 {
				n = 10000
			}
			limit = n
		}
	}

	agentFilter := core.AgentID(r.URL.Query().Get("agent"))
	taskTypeFilter := r.URL.Query().Get("type")
	outputFormat := r.URL.Query().Get("format")
	if outputFormat == "" {
		outputFormat = "json"
	}

	// ── Authentication / billing ─────────────────────────────────
	fromAddr := r.URL.Query().Get("from_address")
	if fromAddr != "" {
		// Check if caller is a registered agent
		_, regErr := s.registry.GetAgentByAddress(core.Address(fromAddr))
		if regErr != nil {
			// External caller — charge 10 $ALPHA
			const exportFee = core.Amount(10)
			if s.ledger != nil {
				if err := s.ledger.BurnSupply(core.Address(fromAddr), exportFee); err != nil {
					writeError(w, http.StatusPaymentRequired,
						"export costs 10 $ALPHA for unregistered callers: "+err.Error())
					return
				}
			}
		}
	}

	// ── Gather records ───────────────────────────────────────────
	if s.oracle == nil {
		writeError(w, http.StatusServiceUnavailable, "intelligence oracle not available")
		return
	}

	records := s.oracle.ExportRecords(limit, agentFilter, taskTypeFilter)

	// ── Respond ──────────────────────────────────────────────────
	switch outputFormat {
	case "jsonl":
		// Newline-delimited JSON — efficient for streaming / bulk import
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		for _, rec := range records {
			if err := enc.Encode(rec); err != nil {
				return // client disconnected
			}
		}
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"count":      len(records),
			"limit":      limit,
			"agent":      string(agentFilter),
			"task_type":  taskTypeFilter,
			"format":     outputFormat,
			"records":    records,
			"exported_at": time.Now().Unix(),
			"chain_id":   "alpha-1",
		})
	}
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
