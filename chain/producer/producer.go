// Package producer implements the Alpha Network block production loop.
// Blocks are produced every 500ms. Each block:
//   1. Collects pending transactions from the mempool
//   2. Runs PoI consensus (if validators are present, otherwise uses a genesis producer)
//   3. Distributes block rewards via the ledger
//   4. Appends the block to the in-memory chain
package producer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alpha-network/alpha/chain/consensus"
	"github.com/alpha-network/alpha/chain/core"
	"github.com/alpha-network/alpha/chain/governance"
	"github.com/alpha-network/alpha/chain/ledger"
	"github.com/alpha-network/alpha/chain/net"
	"github.com/alpha-network/alpha/chain/store"
	"github.com/alpha-network/alpha/chain/tasks"
)

const (
	blockInterval    = 500 * time.Millisecond
	mempoolCap       = 10_000
	rewardAddr       = core.Address("alpha1_protocol_treasury")
	statsLogInterval = 10 * time.Second
)

// ChainStats is a snapshot of current chain performance metrics
type ChainStats struct {
	Height       uint64  `json:"height"`
	BlocksPerSec float64 `json:"blocks_per_sec"`
	TxCount      uint64  `json:"tx_count"`
	AgentCount   int     `json:"agent_count"`
	Uptime       string  `json:"uptime"`
}

// BlockProducer drives the block production loop for Alpha Network
type BlockProducer struct {
	mu          sync.RWMutex
	chain       []*core.Block
	mempool     []*core.Transaction
	mempoolMu   sync.Mutex
	poiEngine   *consensus.PoIEngine
	ledger      *ledger.Ledger
	agentCount  int // set externally via SetAgentCount

	// Phase 2: persistent store, task marketplace, WS hub (all optional)
	store       *store.Store
	marketplace *tasks.Marketplace
	hub         *net.Hub

	// Phase 4: P2P block broadcasting
	p2pBroadcaster BlockBroadcaster

	// Phase 4: Governance
	govModule *governance.Module

	// atomic counters for lock-free stat reads
	height  uint64
	txCount uint64

	startTime time.Time
	started   int32 // atomic flag
}

// NewBlockProducer creates a producer wired to a PoI engine and ledger
func NewBlockProducer(poi *consensus.PoIEngine, l *ledger.Ledger) *BlockProducer {
	genesis := &core.Block{
		Height:       0,
		Timestamp:    time.Now().UnixMilli(),
		PrevHash:     "0000000000000000000000000000000000000000000000000000000000000000",
		Transactions: []*core.Transaction{},
		ValidatorID:  core.AgentID("genesis"),
		PoIProof:     nil,
	}
	genesis.ComputeHash()

	return &BlockProducer{
		chain:     []*core.Block{genesis},
		mempool:   make([]*core.Transaction, 0, 256),
		poiEngine: poi,
		ledger:    l,
		height:    0,
	}
}

// SetStore wires a BadgerDB store for block persistence.
func (p *BlockProducer) SetStore(s *store.Store) {
	p.mu.Lock()
	p.store = s
	p.mu.Unlock()
}

// SetGovModule wires the governance module for per-block state advancement.
func (p *BlockProducer) SetGovModule(g *governance.Module) {
	p.mu.Lock()
	p.govModule = g
	p.mu.Unlock()
}

// SetMarketplace wires the task marketplace for per-block task assignment.
func (p *BlockProducer) SetMarketplace(m *tasks.Marketplace) {
	p.mu.Lock()
	p.marketplace = m
	p.mu.Unlock()
}

// SetHub wires the WebSocket hub for real-time block broadcasts.
func (p *BlockProducer) SetHub(h *net.Hub) {
	p.mu.Lock()
	p.hub = h
	p.mu.Unlock()
}

// getHub returns the WebSocket hub in a thread-safe manner.
func (p *BlockProducer) getHub() *net.Hub {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.hub
}

// BlockBroadcaster is called when a new block is produced locally.
type BlockBroadcaster func(block *core.Block)

// SetP2PBroadcaster wires a callback for broadcasting blocks to P2P peers.
func (p *BlockProducer) SetP2PBroadcaster(bc BlockBroadcaster) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.p2pBroadcaster = bc
}
// local chain. The block must be the next expected height and its PrevHash must
// match the current chain tip.
//
// Returns nil on success, or an error if the block cannot be incorporated
// (wrong height, hash mismatch, etc.).
func (p *BlockProducer) IncorporateExternalBlock(block *core.Block) error {
	if block == nil {
		return fmt.Errorf("nil block")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	currentTip := p.chain[len(p.chain)-1]

	// Height must be exactly tip+1
	if block.Height != currentTip.Height+1 {
		// If it's an older block or same height, it's stale
		if block.Height <= currentTip.Height {
			return fmt.Errorf("stale block at height %d (current tip %d)", block.Height, currentTip.Height)
		}
		// Future block — we need to sync; signal caller to do full sync
		return fmt.Errorf("future block at height %d (current tip %d)", block.Height, currentTip.Height)
	}

	// PrevHash must match current tip's hash
	if block.PrevHash != currentTip.Hash {
		return fmt.Errorf("prev hash mismatch: expected %s, got %s", currentTip.Hash[:16], block.PrevHash[:16])
	}

	// Verify block hash
	storedHash := block.Hash
	block.ComputeHash()
	if storedHash != block.Hash {
		return fmt.Errorf("block hash invalid: stored %s, computed %s", storedHash[:16], block.Hash[:16])
	}

	// Append to chain
	p.chain = append(p.chain, block)
	atomic.StoreUint64(&p.height, block.Height)

	// Persist to store
	if p.store != nil {
		_ = p.store.PutBlock(block)
	}

	// Broadcast via WebSocket
	if p.hub != nil {
		p.hub.BroadcastBlock(block)
	}

	return nil
}

// SetAgentCount updates the live agent count shown in stats
func (p *BlockProducer) SetAgentCount(n int) {
	p.mu.Lock()
	p.agentCount = n
	p.mu.Unlock()
}

// Start launches the block production goroutine. It runs until ctx is cancelled.
// Calling Start more than once is a no-op.
func (p *BlockProducer) Start(ctx context.Context) {
	if !atomic.CompareAndSwapInt32(&p.started, 0, 1) {
		return
	}
	p.startTime = time.Now()

	go p.loop(ctx)
}

// SubmitTransaction adds a transaction to the mempool.
// Returns an error if the mempool is full.
func (p *BlockProducer) SubmitTransaction(tx *core.Transaction) error {
	p.mempoolMu.Lock()
	defer p.mempoolMu.Unlock()

	if len(p.mempool) >= mempoolCap {
		return fmt.Errorf("mempool full (%d transactions)", mempoolCap)
	}
	if tx.TxID == "" {
		tx.TxID = genTxID(tx)
	}
	if tx.Timestamp == 0 {
		tx.Timestamp = time.Now().UnixMilli()
	}
	p.mempool = append(p.mempool, tx)
	return nil
}

// GetBlock returns the block at the given height, or nil if out of range
func (p *BlockProducer) GetBlock(height uint64) *core.Block {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if int(height) >= len(p.chain) {
		return nil
	}
	return p.chain[height]
}

// LatestBlock returns the current chain tip block
func (p *BlockProducer) LatestBlock() *core.Block {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.chain) == 0 {
		return nil
	}
	return p.chain[len(p.chain)-1]
}

// GetChainHeight returns the current block height (tip)
func (p *BlockProducer) GetChainHeight() uint64 {
	return atomic.LoadUint64(&p.height)
}

// GetChainStats returns a live snapshot of chain performance
func (p *BlockProducer) GetChainStats() *ChainStats {
	h := atomic.LoadUint64(&p.height)
	txCount := atomic.LoadUint64(&p.txCount)
	elapsed := time.Since(p.startTime).Seconds()
	bps := 0.0
	if elapsed > 0 {
		bps = float64(h) / elapsed
	}
	p.mu.RLock()
	agents := p.agentCount
	p.mu.RUnlock()

	return &ChainStats{
		Height:       h,
		BlocksPerSec: bps,
		TxCount:      txCount,
		AgentCount:   agents,
		Uptime:       time.Since(p.startTime).Round(time.Second).String(),
	}
}

// --- internal block production loop ---

func (p *BlockProducer) loop(ctx context.Context) {
	ticker := time.NewTicker(blockInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.produceBlock()
		}
	}
}

func (p *BlockProducer) produceBlock() {
	// Drain the mempool
	p.mempoolMu.Lock()
	txs := make([]*core.Transaction, len(p.mempool))
	copy(txs, p.mempool)
	p.mempool = p.mempool[:0]
	p.mempoolMu.Unlock()

	nextHeight := atomic.LoadUint64(&p.height) + 1

	// Get previous block hash
	p.mu.RLock()
	prevBlock := p.chain[len(p.chain)-1]
	p.mu.RUnlock()

	// Run PoI consensus — may fail if no validators registered yet;
	// in that case use a synthetic "genesis producer" proof
	result, _ := p.poiEngine.RunConsensus(nextHeight)

	var validatorID core.AgentID
	var poiProof *core.PoIProof

	if result != nil {
		validatorID = result.ValidatorID

		// Apply rewards to the ledger
		for agentID, reward := range result.Rewards {
			agentAddr := core.Address("alpha_agent_" + string(agentID))
			_ = p.ledger.Credit(agentAddr, reward)
		}
		// Apply slashes (deduct from agent balance, best-effort)
		for agentID, slash := range result.Slashes {
			agentAddr := core.Address("alpha_agent_" + string(agentID))
			if err := p.ledger.Debit(agentAddr, slash); err == nil && slash > 0 {
				// Broadcast slash event via WebSocket
				if h := p.getHub(); h != nil {
					h.BroadcastAgentEvent(net.AgentEvent{
						Type:    "slash",
						AgentID: agentID,
						Payload: map[string]interface{}{"amount": int64(slash)},
						At:      time.Now().Unix(),
					})
				}
			}
		}
	} else {
		// No consensus quorum — produce block with synthetic validator
		validatorID = core.AgentID("genesis-producer")
		// Still distribute a base block reward to the treasury
		blockReward := consensus.RewardForBlock(nextHeight)
		_ = p.ledger.Credit(rewardAddr, blockReward)
	}

	// Apply ledger transfers for submitted transactions
	for _, tx := range txs {
		if tx.Type == core.TxTransfer && tx.Amount > 0 {
			_, _ = p.ledger.Transfer(tx.From, tx.To, tx.Amount, tx.Memo)
		}
		atomic.AddUint64(&p.txCount, 1)
	}

	// Build the proof for the block
	poiProof = buildSyntheticProof(validatorID, nextHeight)

	block := &core.Block{
		Height:       nextHeight,
		Timestamp:    time.Now().UnixMilli(),
		PrevHash:     prevBlock.Hash,
		Transactions: txs,
		ValidatorID:  validatorID,
		PoIProof:     poiProof,
	}
	block.ComputeHash()

	p.mu.Lock()
	p.chain = append(p.chain, block)
	s := p.store
	mp := p.marketplace
	hub := p.hub
	p.mu.Unlock()

	atomic.StoreUint64(&p.height, nextHeight)

	// Persist block to BadgerDB (non-blocking; log but don't crash on error)
	if s != nil {
		if err := s.PutBlock(block); err != nil {
			// Log error but don't disrupt block production
			_ = err
		}

		// Periodic ledger snapshot every 100 blocks for faster recovery
		if nextHeight%100 == 0 {
			balances := p.ledger.SnapshotBalances()
			if err := s.PutSnapshot(nextHeight, balances, p.ledger.TotalBurned(), p.ledger.TotalSupply()); err != nil {
				// Non-fatal: snapshot failure doesn't halt block production
				_ = err
			}
		}
	}

	// Assign pending tasks to the block validator each block
	if mp != nil && validatorID != core.AgentID("genesis-producer") {
		for _, cap := range []core.Capability{core.CapabilityInference, core.CapabilityValidation, core.CapabilityData} {
			task, err := mp.AssignTask(validatorID, cap)
			if err != nil {
				break // no more tasks for this capability
			}
			if task != nil {
				// Record assignment as a transaction on-chain
				_ = p.SubmitTransaction(&core.Transaction{
					Type:  core.TxTaskComplete,
					From:  core.Address(validatorID),
					Memo:  "task_assigned:" + task.TaskID,
				})
			}
		}
	}

	// Broadcast block via WebSocket hub
	if hub != nil {
		hub.BroadcastBlock(block)
	}

	// Broadcast block to P2P peers
	if p.p2pBroadcaster != nil {
		p.p2pBroadcaster(block)
	}

	// Advance governance state machine
	if p.govModule != nil {
		p.govModule.Tick(nextHeight)
	}
}

// buildSyntheticProof creates a simple PoI proof for blocks with no active validators
func buildSyntheticProof(agentID core.AgentID, blockHeight uint64) *core.PoIProof {
	reveal := fmt.Sprintf("synthetic:%s:%d:%d", agentID, blockHeight, time.Now().UnixNano())
	h := sha256.Sum256([]byte(reveal))
	commitment := hex.EncodeToString(h[:])

	return &core.PoIProof{
		AgentID:        agentID,
		BlockHeight:    blockHeight,
		CommitmentHash: commitment,
		RevealProof:    reveal,
		LatencyMs:      250, // synthetic latency
		Signature:      "synthetic",
	}
}

func genTxID(tx *core.Transaction) string {
	data, _ := json.Marshal(tx)
	data = append(data, []byte(fmt.Sprintf("%d", time.Now().UnixNano()))...)
	h := sha256.Sum256(data)
	return "tx_" + hex.EncodeToString(h[:])[:24]
}
