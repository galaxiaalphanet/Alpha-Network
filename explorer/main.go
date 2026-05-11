// Alpha Network Block Explorer
// Standalone web UI that connects to a running Alpha Network node.
// Port: 8082  |  API backend: localhost:8080  |  WS backend: localhost:8081
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ─── Config ───────────────────────────────────────────────────────────────────

var (
	listenAddr = flag.String("addr", ":8082", "explorer listen address")
	apiBase    = flag.String("api", "http://localhost:8080", "Alpha Network API base URL")
	wsBase     = flag.String("ws", "ws://localhost:8081", "Alpha Network WebSocket base URL")
)

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func apiGet(path string) (map[string]interface{}, error) {
	url := *apiBase + path
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func apiGetSlice(path string, key string) ([]interface{}, error) {
	data, err := apiGet(path)
	if err != nil {
		return nil, err
	}
	if v, ok := data[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			return arr, nil
		}
	}
	return []interface{}{}, nil
}

// ─── Templates ────────────────────────────────────────────────────────────────

const baseCSS = `
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'Courier New',Courier,monospace;background:#0a0a0f;color:#c8d8e8;min-height:100vh}
a{color:#4af;text-decoration:none}a:hover{color:#8cf;text-decoration:underline}
nav{background:#0d0d1a;border-bottom:1px solid #1a2a3a;padding:12px 24px;display:flex;align-items:center;gap:24px}
nav .brand{color:#4af;font-size:1.1em;font-weight:bold;letter-spacing:2px}
nav a{color:#8a9ab0;font-size:.85em}nav a:hover{color:#4af}
nav a.active{color:#4af;border-bottom:1px solid #4af;padding-bottom:2px}
.container{max-width:1200px;margin:0 auto;padding:24px}
h1{color:#4af;font-size:1.3em;letter-spacing:1px;margin-bottom:20px;border-bottom:1px solid #1a2a3a;padding-bottom:10px}
h2{color:#6ad;font-size:1em;letter-spacing:1px;margin:20px 0 12px}
.card{background:#0d1117;border:1px solid #1a2a3a;border-radius:6px;padding:16px;margin-bottom:16px}
.card-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;margin-bottom:20px}
.stat{background:#0d1117;border:1px solid #1a2a3a;border-radius:6px;padding:14px;text-align:center}
.stat-val{font-size:1.6em;color:#4af;font-weight:bold;margin-bottom:4px}
.stat-label{font-size:.75em;color:#5a6a7a;text-transform:uppercase;letter-spacing:1px}
table{width:100%;border-collapse:collapse;font-size:.85em}
thead tr{border-bottom:1px solid #1a2a3a}
th{color:#5a6a7a;text-transform:uppercase;font-size:.75em;letter-spacing:1px;padding:8px 12px;text-align:left}
td{padding:8px 12px;border-bottom:1px solid #111820;color:#b0c0d0;vertical-align:top}
tr:hover td{background:#0d1520}
.badge{display:inline-block;padding:2px 8px;border-radius:3px;font-size:.75em;font-weight:bold}
.badge-green{background:#0f2a1a;color:#3d9}
.badge-blue{background:#0a1a2a;color:#4af}
.badge-yellow{background:#1a1a0a;color:#ba3}
.badge-red{background:#2a0a0a;color:#f44}
.mono{font-family:'Courier New',monospace;font-size:.85em;color:#7ab}
.addr{font-family:'Courier New',monospace;font-size:.8em;color:#4af;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;display:inline-block}
.tag{background:#0a1a2a;color:#4af;border:1px solid #1a3a5a;border-radius:3px;padding:1px 6px;font-size:.75em;margin:2px}
.err{color:#f44;font-size:.9em;padding:20px;text-align:center}
.ts{color:#5a6a7a;font-size:.8em}
footer{text-align:center;padding:24px;color:#2a3a4a;font-size:.75em;border-top:1px solid #111820;margin-top:40px}
.live-dot{width:8px;height:8px;background:#3d9;border-radius:50%;display:inline-block;animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.3}}
.status-ok{color:#3d9}
.status-err{color:#f44}
</style>
`

const navHTML = `
<nav>
  <span class="brand">⚡ ALPHA</span>
  <a href="/" {{if eq .Page "dashboard"}}class="active"{{end}}>Dashboard</a>
  <a href="/blocks" {{if eq .Page "blocks"}}class="active"{{end}}>Blocks</a>
  <a href="/agents" {{if eq .Page "agents"}}class="active"{{end}}>Agents</a>
  <a href="/tasks" {{if eq .Page "tasks"}}class="active"{{end}}>Tasks</a>
  <a href="/intelligence" {{if eq .Page "intelligence"}}class="active"{{end}}>Intelligence</a>
  <span style="margin-left:auto;font-size:.75em;color:#2a4a2a"><span class="live-dot"></span> LIVE</span>
</nav>
`

const footerHTML = `<footer>Alpha Network Explorer &mdash; alpha-1 testnet &mdash; PoI consensus</footer>`

// ─── Dashboard (/  ) ──────────────────────────────────────────────────────────

const dashboardTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<title>Alpha Network Explorer</title>
` + baseCSS + `
<script>
function refresh(){
  fetch('/api/chain-info').then(r=>r.json()).then(d=>{
    document.getElementById('height').textContent=d.height||'—';
    document.getElementById('bps').textContent=(d.blocks_per_sec||0).toFixed(2);
    document.getElementById('txs').textContent=d.tx_count||'—';
    document.getElementById('agents').textContent=d.agent_count||'—';
    document.getElementById('circ').textContent=fmt(d.circulating_supply);
    document.getElementById('burned').textContent=fmt(d.total_burned);
    document.getElementById('updated').textContent=new Date().toLocaleTimeString();
  }).catch(()=>{});
}
function fmt(n){if(!n)return'0';if(n>=1e9)return(n/1e9).toFixed(2)+'B';if(n>=1e6)return(n/1e6).toFixed(2)+'M';return n.toString();}
setInterval(refresh,2000);
window.addEventListener('load',refresh);
</script>
</head><body>
` + navHTML + `
<div class="container">
  <h1>Chain Dashboard</h1>
  <div class="card-grid">
    <div class="stat"><div class="stat-val" id="height">{{.Height}}</div><div class="stat-label">Block Height</div></div>
    <div class="stat"><div class="stat-val" id="bps">{{printf "%.2f" .BlocksPerSec}}</div><div class="stat-label">Blocks / sec</div></div>
    <div class="stat"><div class="stat-val" id="txs">{{.TxCount}}</div><div class="stat-label">Total Txs</div></div>
    <div class="stat"><div class="stat-val" id="agents">{{.AgentCount}}</div><div class="stat-label">Active Agents</div></div>
    <div class="stat"><div class="stat-val" id="circ">{{.CirculatingFmt}}</div><div class="stat-label">Circulating $ALPHA</div></div>
    <div class="stat"><div class="stat-val" id="burned">{{.BurnedFmt}}</div><div class="stat-label">Burned $ALPHA</div></div>
  </div>

  <div class="card">
    <h2>Node Status</h2>
    <table>
      <tr><td>Chain ID</td><td class="mono">{{.ChainID}}</td></tr>
      <tr><td>Consensus</td><td><span class="badge badge-blue">{{.Consensus}}</span></td></tr>
      <tr><td>Version</td><td class="mono">{{.Version}}</td></tr>
      <tr><td>Token</td><td class="mono">{{.Token}}</td></tr>
      <tr><td>Total Supply</td><td class="mono">{{.TotalSupply}} $ALPHA</td></tr>
      <tr><td>Network</td><td><span class="badge badge-yellow">{{.Status}}</span></td></tr>
      <tr><td>Last refresh</td><td class="ts" id="updated">—</td></tr>
    </table>
  </div>

  <div class="card">
    <h2>Architecture</h2>
    <pre style="color:#4af;font-size:.82em;padding:8px">
  Agent ──→ API ──→ Chain ──→ Consensus ──→ Ledger ──→ Store
    ↑                              │
    └────── Intelligence Oracle ───┘
    </pre>
  </div>
</div>
` + footerHTML + `
</body></html>`

// ─── Blocks List (/blocks) ────────────────────────────────────────────────────

const blocksListTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<title>Blocks — Alpha Explorer</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
  <h1>Recent Blocks</h1>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="card">
    <table>
      <thead><tr><th>Height</th><th>Timestamp</th><th>Txs</th><th>Validator</th><th>Hash</th></tr></thead>
      <tbody>
      {{range .Blocks}}
      <tr>
        <td><a href="/blocks/{{.Height}}">{{.Height}}</a></td>
        <td class="ts">{{.TimeFmt}}</td>
        <td>{{.TxCount}}</td>
        <td><span class="addr">{{.Validator}}</span></td>
        <td><span class="mono">{{.HashShort}}</span></td>
      </tr>
      {{else}}<tr><td colspan="5" style="text-align:center;color:#5a6a7a">No blocks yet</td></tr>{{end}}
      </tbody>
    </table>
  </div>
  {{end}}
</div>` + footerHTML + `</body></html>`

// ─── Block Detail (/blocks/{height}) ─────────────────────────────────────────

const blockDetailTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<title>Block {{.Height}} — Alpha Explorer</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
  <h1>Block #{{.Height}}</h1>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="card">
    <table>
      <tr><td>Height</td><td class="mono">{{.Height}}</td></tr>
      <tr><td>Timestamp</td><td class="ts">{{.TimeFmt}}</td></tr>
      <tr><td>Validator</td><td><span class="addr">{{.Validator}}</span></td></tr>
      <tr><td>Prev Hash</td><td><span class="mono">{{.PrevHash}}</span></td></tr>
      <tr><td>Hash</td><td><span class="mono">{{.Hash}}</span></td></tr>
      <tr><td>Transactions</td><td>{{.TxCount}}</td></tr>
      {{if .PoISummary}}<tr><td>PoI Proof</td><td><span class="badge badge-green">✓ {{.PoISummary}}</span></td></tr>{{end}}
    </table>
  </div>

  <h2>Transactions ({{.TxCount}})</h2>
  <div class="card">
    <table>
      <thead><tr><th>Tx ID</th><th>Type</th><th>From</th><th>To</th><th>Amount</th></tr></thead>
      <tbody>
      {{range .Txs}}
      <tr>
        <td><span class="mono">{{.TxID}}</span></td>
        <td><span class="badge badge-blue">{{.Type}}</span></td>
        <td><span class="addr">{{.From}}</span></td>
        <td><span class="addr">{{.To}}</span></td>
        <td class="mono">{{.Amount}}</td>
      </tr>
      {{else}}<tr><td colspan="5" style="text-align:center;color:#5a6a7a">No transactions in this block</td></tr>{{end}}
      </tbody>
    </table>
  </div>
  {{end}}
</div>` + footerHTML + `</body></html>`

// ─── Agents Leaderboard (/agents) ─────────────────────────────────────────────

const agentsTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<title>Agents — Alpha Explorer</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
  <h1>Agent Leaderboard</h1>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="card">
    <table>
      <thead><tr><th>#</th><th>Agent ID</th><th>Address</th><th>Reputation</th><th>Stake</th><th>Tasks</th><th>Capabilities</th></tr></thead>
      <tbody>
      {{range $i,$a := .Agents}}
      <tr>
        <td class="ts">{{inc $i}}</td>
        <td><a href="/agents/{{$a.AgentID}}"><span class="mono">{{$a.AgentID}}</span></a></td>
        <td><span class="addr">{{$a.Address}}</span></td>
        <td class="mono">{{$a.Reputation}}</td>
        <td class="mono">{{$a.Stake}}</td>
        <td class="mono">{{$a.Tasks}}</td>
        <td>{{range $a.Caps}}<span class="tag">{{.}}</span>{{end}}</td>
      </tr>
      {{else}}<tr><td colspan="7" style="text-align:center;color:#5a6a7a">No agents registered</td></tr>{{end}}
      </tbody>
    </table>
  </div>
  {{end}}
</div>` + footerHTML + `</body></html>`

// ─── Agent Profile (/agents/{address}) ────────────────────────────────────────

const agentDetailTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<title>Agent {{.AgentID}} — Alpha Explorer</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
  <h1>Agent Profile</h1>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="card">
    <table>
      <tr><td>Agent ID</td><td class="mono">{{.AgentID}}</td></tr>
      <tr><td>Address</td><td><span class="mono">{{.Address}}</span></td></tr>
      <tr><td>Reputation</td><td class="mono">{{.Reputation}}</td></tr>
      <tr><td>Stake</td><td class="mono">{{.Stake}} $ALPHA</td></tr>
      <tr><td>Tasks Completed</td><td class="mono">{{.Tasks}}</td></tr>
      <tr><td>Trust Score</td><td class="mono">{{printf "%.4f" .TrustScore}}</td></tr>
      <tr><td>Balance</td><td class="mono">{{.Balance}} $ALPHA</td></tr>
      <tr><td>Created Block</td><td class="mono">{{.CreatedBlock}}</td></tr>
      <tr><td>Last Active</td><td class="mono">{{.LastActive}}</td></tr>
      <tr><td>Capabilities</td><td>{{range .Caps}}<span class="tag">{{.}}</span>{{end}}</td></tr>
    </table>
  </div>
  {{end}}
</div>` + footerHTML + `</body></html>`

// ─── Tasks (/tasks) ───────────────────────────────────────────────────────────

const tasksTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<title>Tasks — Alpha Explorer</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
  <h1>Task Marketplace</h1>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="card">
    <table>
      <thead><tr><th>Task ID</th><th>Capability</th><th>Reward</th><th>Status</th><th>Posted By</th><th>Deadline</th></tr></thead>
      <tbody>
      {{range .Tasks}}
      <tr>
        <td><span class="mono">{{.TaskID}}</span></td>
        <td><span class="tag">{{.Capability}}</span></td>
        <td class="mono">{{.Reward}} $ALPHA</td>
        <td><span class="badge {{.StatusClass}}">{{.Status}}</span></td>
        <td><span class="addr">{{.PostedBy}}</span></td>
        <td class="ts">{{.DeadlineFmt}}</td>
      </tr>
      {{else}}<tr><td colspan="6" style="text-align:center;color:#5a6a7a">No pending tasks</td></tr>{{end}}
      </tbody>
    </table>
  </div>
  {{end}}
</div>` + footerHTML + `</body></html>`

// ─── Intelligence Oracle (/intelligence) ─────────────────────────────────────

const intelligenceTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<title>Intelligence Oracle — Alpha Explorer</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
  <h1>Intelligence Oracle</h1>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="card-grid">
    <div class="stat"><div class="stat-val">{{.TotalAgents}}</div><div class="stat-label">Total Agents</div></div>
    <div class="stat"><div class="stat-val">{{printf "%.0f" .AvgLatency}}ms</div><div class="stat-label">Avg Latency</div></div>
    <div class="stat"><div class="stat-val">{{printf "%.1f" .ConsensusRate}}%</div><div class="stat-label">Consensus Rate</div></div>
    <div class="stat"><div class="stat-val">{{printf "%.4f" .AvgEntropy}}</div><div class="stat-label">Avg Entropy</div></div>
  </div>
  <div class="card">
    <h2>Network Stats (last 1000 blocks)</h2>
    <table>
      <tr><td>Total Data Points</td><td class="mono">{{.TotalDataPoints}}</td></tr>
      <tr><td>Unique Agents</td><td class="mono">{{.UniqueAgents}}</td></tr>
      <tr><td>Avg Latency</td><td class="mono">{{printf "%.2f" .AvgLatency}}ms</td></tr>
      <tr><td>Consensus Agreement Rate</td><td class="mono">{{printf "%.1f" .ConsensusRate}}%</td></tr>
      <tr><td>Avg Output Entropy</td><td class="mono">{{printf "%.4f" .AvgEntropy}}</td></tr>
      <tr><td>Network Score</td><td class="mono">{{printf "%.4f" .NetworkScore}}</td></tr>
    </table>
  </div>
  {{end}}
</div>` + footerHTML + `</body></html>`

// ─── API proxy endpoint (/api/chain-info) for JS polling ──────────────────────

func handleAPIProxy(w http.ResponseWriter, r *http.Request) {
	data, err := apiGet("/api/v1/chain/info")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"%s"}`, err.Error())
		return
	}
	json.NewEncoder(w).Encode(data)
}

// ─── Template functions ───────────────────────────────────────────────────────

var funcMap = template.FuncMap{
	"inc": func(i int) int { return i + 1 },
}

func renderTmpl(w http.ResponseWriter, tmplStr string, data interface{}) {
	t, err := template.New("page").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("template execute: %v", err)
	}
}

func safeStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return "—"
}

func safeFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}

func safeInt(m map[string]interface{}, key string) int64 {
	f := safeFloat(m, key)
	return int64(f)
}

func fmtAmount(n int64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

func fmtTS(ts int64) string {
	if ts == 0 {
		return "—"
	}
	// try milliseconds first
	t := time.UnixMilli(ts)
	if t.Year() < 2020 {
		t = time.Unix(ts, 0)
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

type pageBase struct {
	Page string
}

// Dashboard
func handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	type dashData struct {
		Page           string
		Height         int64
		BlocksPerSec   float64
		TxCount        int64
		AgentCount     int64
		CirculatingFmt string
		BurnedFmt      string
		ChainID        string
		Consensus      string
		Version        string
		Token          string
		TotalSupply    int64
		Status         string
	}

	info, err := apiGet("/api/v1/chain/info")
	d := dashData{Page: "dashboard"}
	if err == nil {
		d.Height = safeInt(info, "height")
		d.BlocksPerSec = safeFloat(info, "blocks_per_sec")
		d.TxCount = safeInt(info, "tx_count")
		d.AgentCount = safeInt(info, "agent_count")
		d.CirculatingFmt = fmtAmount(safeInt(info, "circulating_supply"))
		d.BurnedFmt = fmtAmount(safeInt(info, "total_burned"))
		d.ChainID = safeStr(info, "chain_id")
		d.Consensus = safeStr(info, "consensus")
		d.Version = safeStr(info, "version")
		d.Token = safeStr(info, "token")
		d.TotalSupply = safeInt(info, "total_supply")
		d.Status = safeStr(info, "status")
	}
	renderTmpl(w, dashboardTmpl, d)
}

// Blocks list
func handleBlocksList(w http.ResponseWriter, r *http.Request) {
	type blockRow struct {
		Height    int64
		TimeFmt   string
		TxCount   int
		Validator string
		HashShort string
	}
	type data struct {
		Page   string
		Blocks []blockRow
		Error  string
	}
	d := data{Page: "blocks"}

	// Fetch latest block height first
	info, err := apiGet("/api/v1/chain/info")
	if err != nil {
		d.Error = "could not connect to Alpha Network node: " + err.Error()
		renderTmpl(w, blocksListTmpl, d)
		return
	}

	topHeight := safeInt(info, "height")
	start := topHeight
	limit := int64(30)
	for i := start; i > start-limit && i >= 0; i-- {
		blk, err := apiGet(fmt.Sprintf("/api/v1/blocks/%d", i))
		if err != nil {
			break
		}
		bm, _ := blk["block"].(map[string]interface{})
		if bm == nil {
			bm = blk
		}
		ts := safeInt(bm, "timestamp")
		validator := safeStr(bm, "validator_id")
		hash := safeStr(bm, "hash")
		if len(hash) > 16 {
			hash = hash[:16] + "…"
		}
		txsRaw, _ := bm["transactions"].([]interface{})
		d.Blocks = append(d.Blocks, blockRow{
			Height:    safeInt(bm, "height"),
			TimeFmt:   fmtTS(ts),
			TxCount:   len(txsRaw),
			Validator: validator,
			HashShort: hash,
		})
	}
	renderTmpl(w, blocksListTmpl, d)
}

// Block detail
func handleBlockDetail(w http.ResponseWriter, r *http.Request) {
	heightStr := strings.TrimPrefix(r.URL.Path, "/blocks/")
	height, err := strconv.ParseInt(heightStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid height", 400)
		return
	}

	type txRow struct {
		TxID   string
		Type   string
		From   string
		To     string
		Amount string
	}
	type data struct {
		Page       string
		Height     int64
		TimeFmt    string
		Validator  string
		PrevHash   string
		Hash       string
		TxCount    int
		PoISummary string
		Txs        []txRow
		Error      string
	}
	d := data{Page: "blocks", Height: height}

	blk, err := apiGet(fmt.Sprintf("/api/v1/blocks/%d", height))
	if err != nil {
		d.Error = "block not found or node unreachable: " + err.Error()
		renderTmpl(w, blockDetailTmpl, d)
		return
	}

	bm, _ := blk["block"].(map[string]interface{})
	if bm == nil {
		bm = blk
	}

	d.Height = safeInt(bm, "height")
	d.TimeFmt = fmtTS(safeInt(bm, "timestamp"))
	d.Validator = safeStr(bm, "validator_id")
	d.PrevHash = safeStr(bm, "prev_hash")
	d.Hash = safeStr(bm, "hash")

	// PoI proof summary
	if poi, ok := bm["poi_proof"].(map[string]interface{}); ok && poi != nil {
		d.PoISummary = fmt.Sprintf("agent=%s latency=%vms", safeStr(poi, "agent_id"), safeStr(poi, "latency_ms"))
	}

	txsRaw, _ := bm["transactions"].([]interface{})
	d.TxCount = len(txsRaw)
	for _, tx := range txsRaw {
		tm, _ := tx.(map[string]interface{})
		if tm == nil {
			continue
		}
		amt := safeInt(tm, "amount")
		d.Txs = append(d.Txs, txRow{
			TxID:   safeStr(tm, "tx_id"),
			Type:   safeStr(tm, "type"),
			From:   safeStr(tm, "from"),
			To:     safeStr(tm, "to"),
			Amount: fmt.Sprintf("%d", amt),
		})
	}
	renderTmpl(w, blockDetailTmpl, d)
}

// Agents leaderboard
func handleAgentsList(w http.ResponseWriter, r *http.Request) {
	type agentRow struct {
		AgentID    string
		Address    string
		Reputation int64
		Stake      int64
		Tasks      int64
		Caps       []string
	}
	type data struct {
		Page   string
		Agents []agentRow
		Error  string
	}
	d := data{Page: "agents"}

	agents, err := apiGetSlice("/api/v1/agents?limit=50", "agents")
	if err != nil {
		d.Error = "could not fetch agents: " + err.Error()
		renderTmpl(w, agentsTmpl, d)
		return
	}

	for _, a := range agents {
		am, _ := a.(map[string]interface{})
		if am == nil {
			continue
		}
		var caps []string
		if capRaw, ok := am["capabilities"].([]interface{}); ok {
			for _, c := range capRaw {
				caps = append(caps, fmt.Sprintf("%v", c))
			}
		}
		d.Agents = append(d.Agents, agentRow{
			AgentID:    safeStr(am, "agent_id"),
			Address:    safeStr(am, "address"),
			Reputation: safeInt(am, "reputation_score"),
			Stake:      safeInt(am, "stake"),
			Tasks:      safeInt(am, "task_count"),
			Caps:       caps,
		})
	}
	renderTmpl(w, agentsTmpl, d)
}

// Agent profile
func handleAgentDetail(w http.ResponseWriter, r *http.Request) {
	agentID := strings.TrimPrefix(r.URL.Path, "/agents/")

	type data struct {
		Page         string
		AgentID      string
		Address      string
		Reputation   int64
		Stake        int64
		Tasks        int64
		TrustScore   float64
		Balance      int64
		CreatedBlock int64
		LastActive   int64
		Caps         []string
		Error        string
	}
	d := data{Page: "agents", AgentID: agentID}

	agent, err := apiGet("/api/v1/agents/" + agentID)
	if err != nil {
		d.Error = "agent not found: " + err.Error()
		renderTmpl(w, agentDetailTmpl, d)
		return
	}

	id, _ := agent["identity"].(map[string]interface{})
	if id == nil {
		id = agent
	}
	d.Address = safeStr(id, "address")
	d.Reputation = safeInt(id, "reputation_score")
	d.Stake = safeInt(id, "stake")
	d.Tasks = safeInt(id, "task_count")
	d.TrustScore = safeFloat(agent, "trust_score")
	d.Balance = safeInt(agent, "balance")
	d.CreatedBlock = safeInt(id, "created_block")
	d.LastActive = safeInt(id, "last_active_block")
	if capRaw, ok := id["capabilities"].([]interface{}); ok {
		for _, c := range capRaw {
			d.Caps = append(d.Caps, fmt.Sprintf("%v", c))
		}
	}
	renderTmpl(w, agentDetailTmpl, d)
}

// Tasks marketplace
func handleTasks(w http.ResponseWriter, r *http.Request) {
	type taskRow struct {
		TaskID      string
		Capability  string
		Reward      int64
		Status      string
		StatusClass string
		PostedBy    string
		DeadlineFmt string
	}
	type data struct {
		Page  string
		Tasks []taskRow
		Error string
	}
	d := data{Page: "tasks"}

	tasks, err := apiGetSlice("/api/v1/tasks", "tasks")
	if err != nil {
		d.Error = "could not fetch tasks: " + err.Error()
		renderTmpl(w, tasksTmpl, d)
		return
	}

	for _, t := range tasks {
		tm, _ := t.(map[string]interface{})
		if tm == nil {
			continue
		}
		status := safeStr(tm, "status")
		statusClass := "badge-blue"
		switch status {
		case "completed":
			statusClass = "badge-green"
		case "pending":
			statusClass = "badge-yellow"
		case "failed":
			statusClass = "badge-red"
		}
		deadline := safeInt(tm, "deadline")
		deadlineFmt := "—"
		if deadline > 0 {
			deadlineFmt = fmtTS(deadline)
		}
		d.Tasks = append(d.Tasks, taskRow{
			TaskID:      safeStr(tm, "task_id"),
			Capability:  safeStr(tm, "capability"),
			Reward:      safeInt(tm, "reward"),
			Status:      status,
			StatusClass: statusClass,
			PostedBy:    safeStr(tm, "posted_by"),
			DeadlineFmt: deadlineFmt,
		})
	}
	renderTmpl(w, tasksTmpl, d)
}

// Intelligence oracle
func handleIntelligence(w http.ResponseWriter, r *http.Request) {
	type data struct {
		Page            string
		TotalAgents     int64
		AvgLatency      float64
		ConsensusRate   float64
		AvgEntropy      float64
		TotalDataPoints int64
		UniqueAgents    int64
		NetworkScore    float64
		Error           string
	}
	d := data{Page: "intelligence"}

	stats, err := apiGet("/api/v1/intelligence/stats")
	if err != nil {
		d.Error = "intelligence oracle unavailable: " + err.Error()
		renderTmpl(w, intelligenceTmpl, d)
		return
	}

	sm, _ := stats["stats"].(map[string]interface{})
	if sm == nil {
		sm = stats
	}

	d.TotalDataPoints = safeInt(sm, "total_data_points")
	d.UniqueAgents = safeInt(sm, "unique_agents")
	d.AvgLatency = safeFloat(sm, "avg_latency_ms")
	d.ConsensusRate = safeFloat(sm, "consensus_rate") * 100
	d.AvgEntropy = safeFloat(sm, "avg_output_entropy")
	d.NetworkScore = safeFloat(sm, "network_intelligence_score")

	// also get agent count from chain info
	info, err2 := apiGet("/api/v1/chain/info")
	if err2 == nil {
		d.TotalAgents = safeInt(info, "agent_count")
	}

	renderTmpl(w, intelligenceTmpl, d)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	flag.Parse()

	log.Printf("⚡ Alpha Network Explorer starting on %s", *listenAddr)
	log.Printf("   API backend: %s", *apiBase)
	log.Printf("   WS backend:  %s", *wsBase)

	mux := http.NewServeMux()

	// Pages
	mux.HandleFunc("/", handleDashboard)
	mux.HandleFunc("/blocks", handleBlocksList)
	mux.HandleFunc("/blocks/", handleBlockDetail)
	mux.HandleFunc("/agents", handleAgentsList)
	mux.HandleFunc("/agents/", handleAgentDetail)
	mux.HandleFunc("/tasks", handleTasks)
	mux.HandleFunc("/intelligence", handleIntelligence)

	// API proxy for JS polling
	mux.HandleFunc("/api/chain-info", handleAPIProxy)

	srv := &http.Server{
		Addr:         *listenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("   Dashboard:   http://localhost:8082/")
	log.Printf("   Blocks:      http://localhost:8082/blocks")
	log.Printf("   Agents:      http://localhost:8082/agents")
	log.Printf("   Tasks:       http://localhost:8082/tasks")
	log.Printf("   Intelligence:http://localhost:8082/intelligence")

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("explorer: %v", err)
	}
}
