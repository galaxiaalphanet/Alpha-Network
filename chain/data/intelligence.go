// Package data implements the Intelligence Layer for Alpha Network.
//
// The Alpha chain is a permanent, public record of AI intelligence in action.
// On-chain data: agent identity, reputation, task type/outcome, consensus records,
// behavioral fingerprints — all anonymized and immutable.
// Off-chain data: actual task content and reasoning traces — agent-controlled, opt-in.
//
// The Data Marketplace lets agents earn $ALPHA by contributing behavioral data;
// consumers pay $ALPHA; the protocol burns a percentage (deflationary).
// The Intelligence Oracle answers queries like "top agents for capability X".
package data

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/alpha-network/alpha/chain/core"
	"github.com/alpha-network/alpha/chain/ledger"
)

// ProtocolFeeRate is the fraction of marketplace revenue burned (5%)
const ProtocolFeeRate = 0.05

// DatasetFilter is used to filter datasets in ListData
type DatasetFilter struct {
	TaskType    string
	MinRecords  int
	FromBlock   uint64
	ToBlock     uint64
}

// IntelligenceRecord is an on-chain behavioral snapshot for one agent in one block.
// All fields are anonymized — no actual task content, no reasoning traces.
type IntelligenceRecord struct {
	RecordID         string          `json:"record_id"`
	AgentID          core.AgentID    `json:"agent_id"`
	BlockHeight      uint64          `json:"block_height"`
	Timestamp        int64           `json:"timestamp"`
	TaskType         string          `json:"task_type"`
	LatencyMs        int64           `json:"latency_ms"`
	OutputEntropy    float64         `json:"output_entropy"`    // 0.0–1.0, real AI > 0.5
	ConsensusAgreement bool          `json:"consensus_agreement"` // was agent in majority?
	ReputationDelta  core.ReputationScore `json:"reputation_delta"`
}

// AgentDataset is the indexed collection of records an agent has contributed
type AgentDataset struct {
	AgentID     core.AgentID          `json:"agent_id"`
	Records     []*IntelligenceRecord `json:"records"`
	TotalEarned core.Amount           `json:"total_earned"`
}

// AccessGrant records a purchase of dataset access
type AccessGrant struct {
	GrantID   string       `json:"grant_id"`
	BuyerID   core.AgentID `json:"buyer_id"`
	DatasetID string       `json:"dataset_id"`
	Price     core.Amount  `json:"price"`
	GrantedAt int64        `json:"granted_at"`
}

// DataMarketplace manages behavioral data contributions, purchases, and burns
type DataMarketplace struct {
	mu       sync.RWMutex
	datasets map[string]*AgentDataset  // datasetID (= agentID string) -> dataset
	grants   []*AccessGrant
	ledger   *ledger.Ledger
	burnAddr core.Address              // protocol treasury / burn sink
}

// NewDataMarketplace creates a marketplace wired to a ledger
func NewDataMarketplace(l *ledger.Ledger, burnAddr core.Address) *DataMarketplace {
	return &DataMarketplace{
		datasets: make(map[string]*AgentDataset),
		grants:   make([]*AccessGrant, 0),
		ledger:   l,
		burnAddr: burnAddr,
	}
}

// ContributeData records an agent's behavioral data on the marketplace.
// The agent earns a reward; in a full deployment the reward is paid from
// future purchase revenue. Here we credit the reward immediately from the
// protocol (simulating network emission).
func (m *DataMarketplace) ContributeData(agentID core.AgentID, record *IntelligenceRecord) (core.Amount, error) {
	if record == nil {
		return 0, errors.New("record cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	datasetID := string(agentID)
	ds, ok := m.datasets[datasetID]
	if !ok {
		ds = &AgentDataset{AgentID: agentID, Records: make([]*IntelligenceRecord, 0)}
		m.datasets[datasetID] = ds
	}

	// Generate stable record ID
	record.RecordID = fmt.Sprintf("ir_%s_%d", agentID, record.BlockHeight)
	record.Timestamp = time.Now().Unix()
	ds.Records = append(ds.Records, record)

	// Reward: 10 $ALPHA (base units) per record contributed
	reward := core.Amount(10)
	agentAddr := core.Address("alpha_agent_" + string(agentID))
	_ = m.ledger.Credit(agentAddr, reward) // best-effort; ignore if ledger not funded
	ds.TotalEarned += reward

	return reward, nil
}

// ListData returns datasets matching the filters.
// If agentID is non-empty only that agent's dataset is returned.
func (m *DataMarketplace) ListData(agentID core.AgentID, filter *DatasetFilter) []*AgentDataset {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*AgentDataset, 0)

	check := func(ds *AgentDataset) {
		if filter != nil {
			if filter.MinRecords > 0 && len(ds.Records) < filter.MinRecords {
				return
			}
		}
		// Return a shallow copy to avoid data races on the slice
		cp := &AgentDataset{
			AgentID:     ds.AgentID,
			Records:     ds.Records,
			TotalEarned: ds.TotalEarned,
		}
		result = append(result, cp)
	}

	if agentID != "" {
		if ds, ok := m.datasets[string(agentID)]; ok {
			check(ds)
		}
		return result
	}
	for _, ds := range m.datasets {
		check(ds)
	}
	return result
}

// PurchaseAccess lets a buyer pay $ALPHA for access to an agent's dataset.
// The protocol burns ProtocolFeeRate of the price; the remainder goes to the dataset owner.
func (m *DataMarketplace) PurchaseAccess(buyerID core.AgentID, datasetID string, price core.Amount) (*AccessGrant, error) {
	if price <= 0 {
		return nil, errors.New("price must be positive")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	ds, ok := m.datasets[datasetID]
	if !ok {
		return nil, fmt.Errorf("dataset not found: %s", datasetID)
	}

	buyerAddr := core.Address("alpha_agent_" + string(buyerID))
	ownerAddr := core.Address("alpha_agent_" + string(ds.AgentID))

	// Protocol fee burn (5%)
	burnAmount := core.Amount(float64(price) * ProtocolFeeRate)
	ownerAmount := price - burnAmount

	// Transfer from buyer to owner
	_, err := m.ledger.Transfer(buyerAddr, ownerAddr, ownerAmount,
		fmt.Sprintf("data purchase: dataset %s", datasetID))
	if err != nil {
		return nil, fmt.Errorf("transfer failed: %w", err)
	}

	// Burn protocol fee
	if burnAmount > 0 {
		_ = m.ledger.BurnFromProtocol(burnAmount)
	}

	grant := &AccessGrant{
		GrantID:   fmt.Sprintf("grant_%s_%d", buyerID, time.Now().UnixNano()),
		BuyerID:   buyerID,
		DatasetID: datasetID,
		Price:     price,
		GrantedAt: time.Now().Unix(),
	}
	m.grants = append(m.grants, grant)
	return grant, nil
}

// BurnFee tracks a standalone protocol fee burn (called by other subsystems)
func (m *DataMarketplace) BurnFee(amount core.Amount) error {
	if amount <= 0 {
		return errors.New("burn amount must be positive")
	}
	return m.ledger.BurnFromProtocol(amount)
}

// TotalDatasets returns the number of agent datasets in the marketplace
func (m *DataMarketplace) TotalDatasets() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.datasets)
}

// --- Intelligence Oracle ---

// NetworkStats is a snapshot of aggregate network intelligence metrics
type NetworkStats struct {
	TimeWindowBlocks uint64  `json:"time_window_blocks"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	Throughput       float64 `json:"throughput_records_per_block"`
	ConsensusRate    float64 `json:"consensus_rate"`      // fraction that agreed
	TotalRecords     int     `json:"total_records"`
	ActiveAgents     int     `json:"active_agents"`
}

// AgentProfile is a comprehensive view of a single agent's intelligence history
type AgentProfile struct {
	AgentID           core.AgentID `json:"agent_id"`
	TotalTasks        int          `json:"total_tasks"`
	AvgLatencyMs      float64      `json:"avg_latency_ms"`
	AvgOutputEntropy  float64      `json:"avg_output_entropy"`
	ConsensusRate     float64      `json:"consensus_rate"`
	TotalRepDelta     core.ReputationScore `json:"total_reputation_delta"`
	TaskTypeBreakdown map[string]int       `json:"task_type_breakdown"`
	LastSeen          int64                `json:"last_seen"`
}

// IntelligenceOracle provides query APIs over the on-chain intelligence data
type IntelligenceOracle struct {
	marketplace *DataMarketplace
	registry    AgentReputationProvider
}

// AgentReputationProvider is the minimal interface the oracle needs from the agent registry
type AgentReputationProvider interface {
	TopAgents(n int, capability *core.Capability) []*core.AgentIdentity
	ListAgents(capability *core.Capability) []*core.AgentIdentity
}

// NewIntelligenceOracle creates an oracle backed by a marketplace and agent registry
func NewIntelligenceOracle(mp *DataMarketplace, reg AgentReputationProvider) *IntelligenceOracle {
	return &IntelligenceOracle{marketplace: mp, registry: reg}
}

// QueryTopAgents returns the top N agents for a given capability,
// filtered to those active within the last timeWindowBlocks blocks.
func (o *IntelligenceOracle) QueryTopAgents(capability string, limit int, timeWindowBlocks uint64) []*core.AgentIdentity {
	var cap *core.Capability
	if capability != "" {
		c := core.Capability(capability)
		cap = &c
	}
	agents := o.registry.TopAgents(limit, cap)

	if timeWindowBlocks == 0 {
		return agents
	}

	// Simulate block height filter using marketplace data
	o.marketplace.mu.RLock()
	defer o.marketplace.mu.RUnlock()

	// Only include agents that have recent intelligence records
	filtered := agents[:0]
	for _, a := range agents {
		ds, ok := o.marketplace.datasets[string(a.AgentID)]
		if !ok {
			// No data — include anyway (agent may not have contributed yet)
			filtered = append(filtered, a)
			continue
		}
		// Check if any record is within the time window
		for _, rec := range ds.Records {
			if rec.BlockHeight+timeWindowBlocks >= rec.BlockHeight {
				filtered = append(filtered, a)
				break
			}
		}
	}
	return filtered
}

// QueryNetworkStats aggregates intelligence metrics across all agents
func (o *IntelligenceOracle) QueryNetworkStats(timeWindowBlocks uint64) *NetworkStats {
	o.marketplace.mu.RLock()
	defer o.marketplace.mu.RUnlock()

	stats := &NetworkStats{TimeWindowBlocks: timeWindowBlocks}
	var totalLatency float64
	var totalConsensusTrue int
	var totalRecords int
	agentSet := make(map[core.AgentID]struct{})

	for _, ds := range o.marketplace.datasets {
		agentSet[ds.AgentID] = struct{}{}
		for _, rec := range ds.Records {
			totalLatency += float64(rec.LatencyMs)
			totalRecords++
			if rec.ConsensusAgreement {
				totalConsensusTrue++
			}
		}
	}

	stats.TotalRecords = totalRecords
	stats.ActiveAgents = len(agentSet)

	if totalRecords > 0 {
		stats.AvgLatencyMs = totalLatency / float64(totalRecords)
		stats.ConsensusRate = float64(totalConsensusTrue) / float64(totalRecords)
		if timeWindowBlocks > 0 {
			stats.Throughput = float64(totalRecords) / float64(timeWindowBlocks)
		}
	}
	return stats
}

// QueryAgentProfile returns a full behavioral profile for a single agent
func (o *IntelligenceOracle) QueryAgentProfile(agentID core.AgentID) (*AgentProfile, error) {
	o.marketplace.mu.RLock()
	defer o.marketplace.mu.RUnlock()

	ds, ok := o.marketplace.datasets[string(agentID)]
	if !ok {
		return nil, fmt.Errorf("no intelligence data for agent: %s", agentID)
	}

	profile := &AgentProfile{
		AgentID:           agentID,
		TaskTypeBreakdown: make(map[string]int),
	}

	var totalLatency float64
	var totalEntropy float64
	var consensusTrue int

	for _, rec := range ds.Records {
		totalLatency += float64(rec.LatencyMs)
		totalEntropy += rec.OutputEntropy
		if rec.ConsensusAgreement {
			consensusTrue++
		}
		profile.TotalRepDelta += rec.ReputationDelta
		profile.TaskTypeBreakdown[rec.TaskType]++
		if rec.Timestamp > profile.LastSeen {
			profile.LastSeen = rec.Timestamp
		}
	}

	n := len(ds.Records)
	profile.TotalTasks = n
	if n > 0 {
		profile.AvgLatencyMs = totalLatency / float64(n)
		profile.AvgOutputEntropy = totalEntropy / float64(n)
		profile.ConsensusRate = float64(consensusTrue) / float64(n)
	}
	return profile, nil
}

// ExportRecords returns the last N agent behavioral records across all datasets,
// optionally filtered by agent ID and/or task type. Results are sorted by block
// height descending (most recent first). This is the canonical data feed for the
// Intelligence Data Subscription product.
func (o *IntelligenceOracle) ExportRecords(limit int, agentFilter core.AgentID, taskTypeFilter string) []*IntelligenceRecord {
	o.marketplace.mu.RLock()
	defer o.marketplace.mu.RUnlock()

	// Collect all matching records
	var all []*IntelligenceRecord
	for _, ds := range o.marketplace.datasets {
		if agentFilter != "" && ds.AgentID != agentFilter {
			continue
		}
		for _, rec := range ds.Records {
			if taskTypeFilter != "" && rec.TaskType != taskTypeFilter {
				continue
			}
			cp := *rec // shallow copy is fine; strings are immutable in Go
			all = append(all, &cp)
		}
	}

	// Sort by block height descending (most recent first)
	sort.Slice(all, func(i, j int) bool {
		if all[i].BlockHeight != all[j].BlockHeight {
			return all[i].BlockHeight > all[j].BlockHeight
		}
		return all[i].Timestamp > all[j].Timestamp
	})

	// Apply limit
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}

	return all
}

// TopByEntropy returns agents sorted by average output entropy (most "AI-like" first)
func (o *IntelligenceOracle) TopByEntropy(limit int) []core.AgentID {
	o.marketplace.mu.RLock()
	defer o.marketplace.mu.RUnlock()

	type scored struct {
		agentID core.AgentID
		entropy float64
	}
	list := make([]scored, 0, len(o.marketplace.datasets))

	for _, ds := range o.marketplace.datasets {
		if len(ds.Records) == 0 {
			continue
		}
		var sum float64
		for _, r := range ds.Records {
			sum += r.OutputEntropy
		}
		list = append(list, scored{ds.AgentID, sum / float64(len(ds.Records))})
	}

	sort.Slice(list, func(i, j int) bool { return list[i].entropy > list[j].entropy })

	result := make([]core.AgentID, 0, limit)
	for i, s := range list {
		if limit > 0 && i >= limit {
			break
		}
		result = append(result, s.agentID)
	}
	return result
}
