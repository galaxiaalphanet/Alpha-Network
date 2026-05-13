// Alpha Network — The first self-evolving economic protocol for AI agents
// Token: $ALPHA | Supply: 1,000,000,000 | Consensus: Proof of Intelligence
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alpha-network/alpha/chain/agent"
	"github.com/alpha-network/alpha/chain/api"
	"github.com/alpha-network/alpha/chain/consensus"
	"github.com/alpha-network/alpha/chain/core"
	"github.com/alpha-network/alpha/chain/data"
	"github.com/alpha-network/alpha/chain/genesis"
	"github.com/alpha-network/alpha/chain/ledger"
	"github.com/alpha-network/alpha/chain/monitor"
	chainnet "github.com/alpha-network/alpha/chain/net"
	"github.com/alpha-network/alpha/chain/producer"
	"github.com/alpha-network/alpha/chain/store"
	"github.com/alpha-network/alpha/chain/tasks"
)

const banner = `
╔══════════════════════════════════════════════════════════════╗
║          ALPHA NETWORK — AI Agent Intelligence Layer         ║
║   Token: $ALPHA  |  Supply: 1,000,000,000  |  PoI v0.3     ║
║   "Bitcoin stores value. Ethereum stores contracts.          ║
║    Alpha stores intelligence."                               ║
╚══════════════════════════════════════════════════════════════╝
`

func main() {
	fmt.Print(banner)

	// ── CLI Flags ─────────────────────────────────────────────────────────────
	dataDir := flag.String("datadir", defaultDataDir(), "data directory for chain state")
	port := flag.Int("port", 8080, "REST API port")
	wsPort := flag.Int("ws-port", 8081, "WebSocket streaming port")
	flag.Parse()

	// Override from environment variables (for Docker / cloud deployments)
	if p := os.Getenv("ALPHA_PORT"); p != "" {
		fmt.Sscanf(p, "%d", port)
	}
	if d := os.Getenv("ALPHA_DATADIR"); d != "" {
		*dataDir = d
	}

	log.Printf("🔷 Data directory: %s", *dataDir)

	// ── 1. Genesis Config ─────────────────────────────────────────────────────
	genesisPath := filepath.Join(*dataDir, "genesis.json")
	genConfig := loadOrCreateGenesis(genesisPath)
	log.Printf("⛓  Chain: %s | Genesis: %s", genConfig.ChainID, genConfig.GenesisTime.Format("2006-01-02"))

	// ── 2. Ledger ─────────────────────────────────────────────────────────────
	l := ledger.NewLedger(core.Amount(genConfig.TotalSupply))

	// ── 3. BadgerDB Store ─────────────────────────────────────────────────────
	storeDir := filepath.Join(*dataDir, "data")
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		log.Fatalf("create store dir: %v", err)
	}
	st, err := store.Open(storeDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("store close error: %v", err)
		}
	}()
	log.Printf("💾 BadgerDB store opened at %s", storeDir)

	// ── 4. Wire ledger persistence to BadgerDB ─────────────────────────────────
	// Every balance change is persisted immediately. On restart, balances
	// survive because they're loaded from BadgerDB.
	l.SetPersisters(
		func(addr core.Address, amount core.Amount) error {
			return st.PutBalance(addr, amount)
		},
		func(key string, value []byte) error {
			return st.PutMeta(key, value)
		},
	)

	// ── 5. Chain Initialization (first run only) ──────────────────────────────
	if !st.HasGenesisBlock() {
		log.Printf("🌱 First run: initializing chain from genesis...")
		if err := genesis.InitChainFromGenesis(genConfig, st, l); err != nil {
			log.Fatalf("init chain: %v", err)
		}
		log.Printf("✅ Chain initialized: %s", genConfig.ChainID)
	} else {
		// Restore ledger balances from BadgerDB
		balances, err := st.ScanBalanceEntries()
		if err != nil {
			log.Printf("⚠️  Could not scan balance entries: %v", err)
		} else if len(balances) > 0 {
			if err := l.LoadBalances(balances); err != nil {
				log.Printf("⚠️  Could not load balances: %v", err)
			}
		}
		// Also restore metadata
		if burned, metaErr := st.GetMeta("total_burned"); metaErr == nil {
			var b int64
			if _, parseErr := fmt.Sscanf(string(burned), "%d", &b); parseErr == nil {
				l.SetTotalBurned(core.Amount(b))
			}
		}
		log.Printf("♻️  Resuming existing chain (genesis block found, %d accounts loaded)", len(balances))
	}

	// ── 6. Agent Registry ─────────────────────────────────────────────────────
	registry := agent.NewRegistry()

	// ── 7. PoI Consensus Engine ───────────────────────────────────────────────
	poiEngine := consensus.NewPoIEngine()

	// ── 8. Task Marketplace ───────────────────────────────────────────────────
	marketplace := tasks.NewMarketplace(l)
	log.Printf("🛒 Task marketplace initialized")

	// ── 9. Block Producer ─────────────────────────────────────────────────────
	prod := producer.NewBlockProducer(poiEngine, l)
	prod.SetStore(st)
	prod.SetMarketplace(marketplace)

	// ── 10. Data / Intelligence Layer ──────────────────────────────────────────
	mp := data.NewDataMarketplace(l, core.Address("alpha1_block_rewards_treasury"))
	oracle := data.NewIntelligenceOracle(mp, registry)

	// ── 11. WebSocket Hub ─────────────────────────────────────────────────────
	hub := chainnet.NewHub()
	prod.SetHub(hub)
	wsAddr := fmt.Sprintf(":%d", *wsPort)
	hub.Start(wsAddr)
	log.Printf("📡 WebSocket hub started on %s/ws", wsAddr)

	// ── 12. API Server ────────────────────────────────────────────────────────
	server := api.NewServerPhase2(registry, l, prod, oracle, marketplace, hub, *port)


	// ── 13. Demo agent ────────────────────────────────────────────────────────
	testAgent, err := registry.RegisterAgent(
		core.Address("alpha1demo000000000000000000000000"),
		[]core.Capability{
			core.CapabilityValidation,
			core.CapabilityInference,
		},
		core.Amount(10_000),
		0,
	)
	if err != nil {
		log.Printf("Warning: demo agent registration: %v", err)
	} else {
		demoAddr := core.Address(testAgent.Address)
		_ = l.Credit(demoAddr, core.Amount(10_000))
		_ = poiEngine.RegisterValidator(testAgent)
		seedIntelligenceData(mp, testAgent.AgentID)
		prod.SetAgentCount(len(registry.ListAgents(nil)))
		log.Printf("✅ Demo agent registered: %s", testAgent.AgentID)
	}

	// ── 14. Seed demo tasks ───────────────────────────────────────────────────
	seedDemoTasks(marketplace)

	// ── 15. Start block producer ──────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prod.Start(ctx)

	// Health Monitor
	mon := monitor.New(prod)
	mon.Start(ctx)
	server.SetMonitor(mon)
	log.Printf("🏥 Health monitor started")
	log.Printf("⛏  Block producer started — target %dms blocks", genConfig.BlockTimeMs)

	// ── 16. Live stats goroutine ──────────────────────────────────────────────
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logLiveStats(prod, l, registry)
			}
		}
	}()

	// ── 17. Graceful shutdown ──────────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("🛑 Shutdown signal received — stopping node…")
		cancel()
		hub.Stop()
		os.Exit(0)
	}()

	// ── 18. Print startup summary ──────────────────────────────────────────────
	log.Printf("🔺 Alpha Network node starting on port %d", *port)
	log.Printf("📡 Chain ID: %s | Consensus: Proof of Intelligence v0.3", genConfig.ChainID)
	log.Printf("💰 Total Supply: %d $ALPHA | Circulating: %d", genConfig.TotalSupply, l.CirculatingSupply())
	log.Printf("")
	log.Printf("Endpoints:")
	log.Printf("  POST /api/v1/agents/register              — Register AI agent")
	log.Printf("  GET  /api/v1/agents                       — List agents")
	log.Printf("  POST /api/v1/transfer                     — Send $ALPHA")
	log.Printf("  GET  /api/v1/chain/info                   — Chain status")
	log.Printf("  GET  /api/v1/blocks/latest                — Latest block")
	log.Printf("  GET  /api/v1/blocks/{height}              — Block by height")
	log.Printf("  GET  /api/v1/accounts/{addr}/balance      — Account balance")
	log.Printf("  POST /api/v1/tasks                        — Post a task")
	log.Printf("  GET  /api/v1/tasks/available?capability=X — Available tasks")
	log.Printf("  GET  /api/v1/tasks/{id}                   — Task status")
	log.Printf("  POST /api/v1/tasks/{id}/submit            — Submit result")
	log.Printf("  GET  /api/v1/intelligence/query           — Oracle query")
	log.Printf("  GET  /api/v1/intelligence/stats           — Network stats")
	log.Printf("  GET  /api/v1/intelligence/top             — Top agents")
	log.Printf("  WS   /ws                                  — Real-time events (port %d)", *wsPort)
	log.Printf("  GET  /health                              — Health check")
	log.Printf("")
	log.Printf("🌐 API: http://localhost:%d/api/v1/chain/info", *port)
	log.Printf("🔌 WS:  ws://localhost:%d/ws", *wsPort)

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// defaultDataDir returns ~/.alpha as the default data directory.
func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".alpha"
	}
	return filepath.Join(home, ".alpha")
}

// loadOrCreateGenesis reads the genesis file or creates a default one.
func loadOrCreateGenesis(path string) *genesis.GenesisConfig {
	cfg, err := genesis.ReadGenesisFile(path)
	if err == nil {
		return cfg
	}

	// File doesn't exist or is invalid — create default
	cfg = genesis.DefaultGenesis()
	if werr := genesis.WriteGenesisFile(path, cfg); werr != nil {
		log.Printf("Warning: could not write genesis file: %v", werr)
	} else {
		log.Printf("📝 Created default genesis config: %s", path)
	}
	return cfg
}

// logLiveStats prints a concise chain status line every 10 seconds
func logLiveStats(prod *producer.BlockProducer, l *ledger.Ledger, reg *agent.Registry) {
	stats := prod.GetChainStats()
	ledgerStats := l.Stats()
	log.Printf(
		"📊 Height: %d | %.1f blk/s | Txs: %d | Agents: %d | Circulating: %v $ALPHA | Burned: %v $ALPHA | Uptime: %s",
		stats.Height,
		stats.BlocksPerSec,
		stats.TxCount,
		len(reg.ListAgents(nil)),
		ledgerStats["circulating_supply"],
		ledgerStats["total_burned"],
		stats.Uptime,
	)
}

// seedIntelligenceData populates the marketplace with initial behavioral records.
func seedIntelligenceData(mp *data.DataMarketplace, agentID core.AgentID) {
	taskTypes := []string{"inference", "validation", "data", "governance"}
	for i, tt := range taskTypes {
		rec := &data.IntelligenceRecord{
			AgentID:            agentID,
			BlockHeight:        uint64(i),
			TaskType:           tt,
			LatencyMs:          int64(250 + i*50),
			OutputEntropy:      0.75 + float64(i)*0.03,
			ConsensusAgreement: true,
			ReputationDelta:    10,
		}
		_, _ = mp.ContributeData(agentID, rec)
	}
}

// seedDemoTasks posts a few example tasks to the marketplace at startup.
func seedDemoTasks(mp *tasks.Marketplace) {
	demotasks := []*core.Task{
		{
			TaskID:     "demo_task_inference_1",
			Reward:     500,
			Capability: core.CapabilityInference,
			InputHash:  "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			TaskID:     "demo_task_validation_1",
			Reward:     200,
			Capability: core.CapabilityValidation,
			InputHash:  "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
		{
			TaskID:     "demo_task_data_1",
			Reward:     100,
			Capability: core.CapabilityData,
			InputHash:  "sha256:fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
		},
	}
	for _, t := range demotasks {
		if err := mp.PostTask(t); err != nil {
			log.Printf("Warning: seed task %s: %v", t.TaskID, err)
		}
	}
	log.Printf("🗂  Seeded %d demo tasks to marketplace", len(demotasks))
}
