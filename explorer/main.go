// Alpha Network Block Explorer
// Standalone web UI that connects to a running Alpha Network node.
// Port: 8082  |  API backend: localhost:8080  |  WS backend: localhost:8081
//
// Design: Solscan/Etherscan-quality dark theme with electric cyan accents.
// JetBrains Mono font, card-based layout, inline block expansion,
// sortable tables, pagination, auto-refresh, responsive mobile layout.
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

// ─── Helper utilities ─────────────────────────────────────────────────────────

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
	t := time.UnixMilli(ts)
	if t.Year() < 2020 {
		t = time.Unix(ts, 0)
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

// ─── Templates ────────────────────────────────────────────────────────────────

const baseCSS = `
<style>
@import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@300;400;500;600;700&family=Inter:wght@300;400;500;600;700&display=swap');
*{box-sizing:border-box;margin:0;padding:0}
html{font-size:15px}
body{font-family:'Inter','Segoe UI',system-ui,-apple-system,sans-serif;background:#0a0a0f;color:#e0e8f0;min-height:100vh;line-height:1.5}
.mono,.mono-all *{font-family:'JetBrains Mono','Courier New',monospace!important}
a{color:#00F5FF;text-decoration:none;transition:color .15s}a:hover{color:#66faff;text-decoration:underline}

/* ── Nav ── */
nav{background:#0d1520;border-bottom:1px solid rgba(0,245,255,0.15);padding:0;display:flex;align-items:center;gap:0;position:sticky;top:0;z-index:100}
nav .brand{font-family:'JetBrains Mono',monospace;color:#00F5FF;font-size:1.05rem;font-weight:700;letter-spacing:1.5px;padding:14px 20px;display:flex;align-items:center;gap:8px}
nav .brand span{font-size:1.2rem}
nav .nav-links{display:flex;align-items:center;gap:0;flex:1}
nav .nav-links a{color:#8899b0;font-size:.82rem;font-weight:500;padding:14px 18px;border-bottom:2px solid transparent;transition:all .15s;text-decoration:none;letter-spacing:.3px}
nav .nav-links a:hover{color:#c0d0e0;background:rgba(0,245,255,0.04)}
nav .nav-links a.active{color:#00F5FF;border-bottom-color:#00F5FF;background:rgba(0,245,255,0.06)}
nav .nav-right{margin-left:auto;display:flex;align-items:center;gap:12px;padding:14px 20px}
nav .nav-right .live-indicator{display:flex;align-items:center;gap:6px;font-size:.72rem;color:#2a6a3a;letter-spacing:.5px;text-transform:uppercase}
nav .nav-search{display:flex;align-items:center;gap:0;margin:0 8px}
nav .nav-search input{background:#060a14;border:1px solid rgba(0,245,255,0.15);border-radius:7px 0 0 7px;padding:7px 12px;color:#c0d8f0;font-family:'JetBrains Mono',monospace;font-size:.73rem;width:220px;outline:none;transition:all .15s}
nav .nav-search input:focus{border-color:#00F5FF;box-shadow:0 0 14px rgba(0,245,255,0.08)}
nav .nav-search input::placeholder{color:#2a3a4a}
nav .nav-search button{background:rgba(0,245,255,0.1);border:1px solid rgba(0,245,255,0.15);border-left:none;color:#00F5FF;padding:7px 11px;border-radius:0 7px 7px 0;cursor:pointer;font-size:.85rem;transition:all .15s;display:flex;align-items:center}
nav .nav-search button:hover{background:rgba(0,245,255,0.18)}
.live-dot{width:7px;height:7px;background:#00cc66;border-radius:50%;display:inline-block;animation:pulse 2s ease-in-out infinite}
@keyframes pulse{0%,100%{opacity:1;box-shadow:0 0 4px #00cc66}50%{opacity:.4;box-shadow:0 0 8px #00cc66}}

/* ── Layout ── */
.container{max-width:1360px;margin:0 auto;padding:28px 24px}
.page-title{font-family:'JetBrains Mono',monospace;font-size:1.15rem;font-weight:600;color:#00F5FF;margin-bottom:24px;padding-bottom:14px;border-bottom:1px solid rgba(0,245,255,0.1);display:flex;align-items:center;gap:10px}
.page-title span{font-size:1.2rem}

/* ── Cards ── */
.card{background:#0d1520;border:1px solid rgba(255,255,255,0.06);border-radius:10px;padding:20px;margin-bottom:18px}
.card h2{font-family:'JetBrains Mono',monospace;font-size:.8rem;font-weight:600;color:#6a8aaa;text-transform:uppercase;letter-spacing:1px;margin-bottom:16px;padding-bottom:10px;border-bottom:1px solid rgba(255,255,255,0.05)}
.card-title-row{display:flex;justify-content:space-between;align-items:center;margin-bottom:12px}
.card-subtitle{font-size:.78rem;color:#5a6a7a}

/* ── Stat Cards Grid ── */
.stat-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:14px;margin-bottom:22px}
.stat-card{background:linear-gradient(135deg,#0d1520 0%,#111a2a 100%);border:1px solid rgba(0,245,255,0.08);border-radius:10px;padding:18px 16px;position:relative;overflow:hidden;transition:all .2s}
.stat-card:hover{border-color:rgba(0,245,255,0.2);box-shadow:0 0 20px rgba(0,245,255,0.06)}
.stat-card .icon{font-size:1.5rem;margin-bottom:6px}
.stat-card .value{font-family:'JetBrains Mono',monospace;font-size:1.6rem;font-weight:700;color:#00F5FF;line-height:1.2}
.stat-card .label{font-size:.72rem;color:#5a7a9a;text-transform:uppercase;letter-spacing:.8px;margin-top:4px}
.stat-card .glow{position:absolute;top:-50%;right:-20%;width:100px;height:200px;background:radial-gradient(ellipse,rgba(0,245,255,0.04) 0%,transparent 70%);pointer-events:none}

/* ── Tables ── */
.table-wrap{overflow-x:auto}
table{width:100%;border-collapse:collapse;font-size:.85rem}
thead tr{border-bottom:1px solid rgba(255,255,255,0.06)}
th{font-family:'Inter',sans-serif;color:#5a7a9a;font-size:.7rem;font-weight:600;text-transform:uppercase;letter-spacing:.8px;padding:10px 14px;text-align:left;white-space:nowrap;cursor:pointer;user-select:none;transition:color .15s}
th:hover{color:#00F5FF}
th .sort-arrow{display:inline-block;margin-left:4px;color:#00F5FF;font-size:.65rem;opacity:.4}
th .sort-arrow.active{opacity:1}
td{padding:10px 14px;border-bottom:1px solid rgba(255,255,255,0.04);color:#c8d8e8;vertical-align:middle;font-size:.82rem}
tbody tr{transition:background .12s}
tbody tr:hover{background:rgba(0,245,255,0.03)}
tbody tr:nth-child(even){background:rgba(255,255,255,0.015)}
tbody tr.clickable{cursor:pointer}
tbody tr.clickable:hover{background:rgba(0,245,255,0.06)}

/* ── Expandable Block Row ── */
tr.expanded{background:rgba(0,245,255,0.04)!important}
tr.expanded + tr.block-detail-row{display:table-row}
tr.block-detail-row{display:none}
tr.block-detail-row td{padding:0}
tr.block-detail-row .block-detail-inner{background:#0a0f1a;border:1px solid rgba(0,245,255,0.1);border-radius:8px;margin:4px 14px 10px;padding:14px 16px;display:grid;grid-template-columns:140px 1fr;gap:4px 12px;font-size:.8rem}
tr.block-detail-row .detail-label{color:#5a7a9a;font-family:'JetBrains Mono',monospace;font-size:.72rem;text-transform:uppercase;letter-spacing:.5px}
tr.block-detail-row .detail-value{color:#c0d0e0;font-family:'JetBrains Mono',monospace;font-size:.78rem;word-break:break-all}

/* ── Badges ── */
.badge{display:inline-flex;align-items:center;gap:4px;padding:3px 10px;border-radius:5px;font-size:.72rem;font-weight:600;letter-spacing:.3px;line-height:1.4}
.badge-cyan{background:rgba(0,245,255,0.1);color:#00F5FF}
.badge-green{background:rgba(0,204,102,0.1);color:#00cc66}
.badge-blue{background:rgba(68,170,255,0.1);color:#4af}
.badge-gold{background:rgba(255,204,0,0.12);color:#fc0}
.badge-red{background:rgba(255,68,68,0.1);color:#f44}
.badge-gray{background:rgba(160,170,180,0.1);color:#a0aab4}
.badge-purple{background:rgba(170,102,255,0.1);color:#a6f}
.badge-orange{background:rgba(255,153,51,0.12);color:#f93}

/* Tier badges */
.tier-seed{background:rgba(140,150,160,0.1);color:#8c96a0}
.tier-active{background:rgba(68,170,255,0.1);color:#4af}
.tier-trusted{background:rgba(0,204,102,0.1);color:#00cc66}
.tier-elite{background:rgba(255,204,0,0.12);color:#fc0}

/* ── Pagination ── */
.pagination{display:flex;align-items:center;justify-content:center;gap:12px;margin-top:18px;padding:8px 0}
.pagination button{background:rgba(0,245,255,0.08);border:1px solid rgba(0,245,255,0.15);color:#00F5FF;padding:6px 16px;border-radius:6px;cursor:pointer;font-size:.78rem;font-weight:500;transition:all .15s;font-family:'Inter',sans-serif}
.pagination button:hover:not(:disabled){background:rgba(0,245,255,0.15);border-color:rgba(0,245,255,0.3)}
.pagination button:disabled{opacity:.3;cursor:not-allowed;background:transparent;border-color:rgba(255,255,255,0.06);color:#5a6a7a}
.pagination .page-info{font-family:'JetBrains Mono',monospace;font-size:.78rem;color:#6a8aaa}

/* ── Search ── */
.search-bar{display:flex;gap:8px;margin-bottom:16px}
.search-bar input{flex:1;background:#0a0f1a;border:1px solid rgba(0,245,255,0.12);border-radius:8px;padding:10px 14px;color:#e0e8f0;font-family:'JetBrains Mono',monospace;font-size:.8rem;outline:none;transition:border-color .15s}
.search-bar input:focus{border-color:#00F5FF;box-shadow:0 0 12px rgba(0,245,255,0.08)}
.search-bar input::placeholder{color:#3a4a5a}
.search-bar button{background:rgba(0,245,255,0.1);border:1px solid rgba(0,245,255,0.15);color:#00F5FF;padding:10px 18px;border-radius:8px;cursor:pointer;font-family:'JetBrains Mono',monospace;font-size:.8rem;transition:all .15s}
.search-bar button:hover{background:rgba(0,245,255,0.18);border-color:rgba(0,245,255,0.3)}

/* ── Loading indicator ── */
.loading-bar{height:3px;background:transparent;position:fixed;top:0;left:0;width:100%;z-index:200;pointer-events:none;transition:opacity .3s}
.loading-bar.active{background:linear-gradient(90deg,transparent,#00F5FF,transparent);background-size:200% 100%;animation:loadingSlide 1.2s ease-in-out infinite}
@keyframes loadingSlide{0%{background-position:-200% 0}100%{background-position:200% 0}}
.loading-dot{display:inline-block;width:5px;height:5px;background:#00F5FF;border-radius:50%;margin:0 3px;opacity:0;transition:opacity .3s}
.loading-dot.active{opacity:1;animation:dotPulse 1s ease-in-out infinite}
@keyframes dotPulse{0%,100%{opacity:.3;transform:scale(1)}50%{opacity:1;transform:scale(1.3)}}

/* ── Explorer detail labels ── */
.info-grid{display:grid;grid-template-columns:160px 1fr;gap:8px 16px;font-size:.82rem}
.info-label{font-family:'JetBrains Mono',monospace;font-size:.72rem;color:#5a7a9a;text-transform:uppercase;letter-spacing:.5px;padding:6px 0}
.info-value{color:#c8d8e8;padding:6px 0;word-break:break-all;font-family:'JetBrains Mono',monospace;font-size:.78rem}
.info-value .text{font-family:'Inter',sans-serif}

/* ── List detail (blocks) ── */
.hash-trunc{font-family:'JetBrains Mono',monospace;font-size:.75rem;color:#00F5FF;max-width:160px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;display:inline-block;vertical-align:middle}
.hash-full{font-family:'JetBrains Mono',monospace;font-size:.75rem;color:#6a8aaa;word-break:break-all}
.addr{font-family:'JetBrains Mono',monospace;font-size:.75rem;color:#6a8aaa;word-break:break-all}
.mono{font-family:'JetBrains Mono',monospace}
.ts{color:#5a6a7a;font-size:.75rem;font-family:'JetBrains Mono',monospace}

/* ── Footer ── */
footer{text-align:center;padding:28px 24px;color:#1a2a3a;font-size:.72rem;border-top:1px solid rgba(255,255,255,0.04);margin-top:40px;letter-spacing:.5px}
footer a{color:#2a4a6a}
footer a:hover{color:#4af}

/* ── Error ── */
.err{padding:40px 20px;text-align:center;color:#f44;font-size:.9rem;background:rgba(255,68,68,0.04);border:1px solid rgba(255,68,68,0.1);border-radius:10px}

/* ── Responsive ── */
@media(max-width:768px){
  nav{flex-wrap:wrap}
  nav .brand{padding:10px 14px;font-size:.9rem}
  nav .nav-links{order:3;width:100%;overflow-x:auto;gap:0}
  nav .nav-links a{padding:10px 14px;font-size:.75rem;white-space:nowrap}
  nav .nav-search{order:2;width:calc(100% - 100px);margin:0 6px}
  nav .nav-search input{width:100%}
  nav .nav-right{margin-left:0;padding:8px 14px;margin-left:auto}
  .container{padding:16px 12px}
  .stat-grid{grid-template-columns:repeat(2,1fr);gap:10px}
  .stat-card .value{font-size:1.2rem}
  .info-grid{grid-template-columns:1fr;gap:2px}
  .page-title{font-size:1rem}
  th,td{padding:8px 8px;font-size:.75rem}
  .hash-trunc{max-width:90px}
}

@media(max-width:480px){
  .stat-grid{grid-template-columns:1fr 1fr;gap:8px}
  .stat-card{padding:12px 10px}
  .stat-card .value{font-size:1rem}
  .stat-card .icon{font-size:1.1rem}
}
</style>
`

const navHTML = `
<div class="loading-bar" id="loadingBar"></div>
<nav>
  <a href="/" class="brand"><span>⚡</span> ALPHA</a>
  <div class="nav-links">
    <a href="/" {{if eq .Page "dashboard"}}class="active"{{end}}>Dashboard</a>
    <a href="/blocks" {{if eq .Page "blocks"}}class="active"{{end}}>Blocks</a>
    <a href="/agents" {{if eq .Page "agents"}}class="active"{{end}}>Agents</a>
    <a href="/tasks" {{if eq .Page "tasks"}}class="active"{{end}}>Tasks</a>
    <a href="/intelligence" {{if eq .Page "intelligence"}}class="active"{{end}}>Intelligence</a>
  </div>
  <div class="nav-search">
    <input type="text" id="globalSearch" placeholder="Search block, agent, task…" onkeydown="if(event.key==='Enter')doSearch()" autocomplete="off">
    <button onclick="doSearch()" aria-label="Search">&#128269;</button>
  </div>
  <div class="nav-right">
    <div class="live-indicator"><span class="live-dot"></span> LIVE</div>
  </div>
</nav>
<script>
function doSearch(){
  var v=document.getElementById('globalSearch').value.trim();
  if(!v)return;
  if(/^\d+$/.test(v)){window.location.href='/blocks/'+v;return}
  if(/^alpha1/.test(v)){window.location.href='/agents/'+encodeURIComponent(v);return}
  if(/^[0-9a-fA-F]{64}$/.test(v)){window.location.href='/blocks?tx='+v;return}
  window.location.href='/agents/'+encodeURIComponent(v);
}
</script>
`

const footerHTML = `<footer><a href="/">Alpha Network Explorer</a> &mdash; alpha-1 testnet &mdash; <span class="mono">Proof of Intelligence</span></footer>`

// ─── Dashboard (/  ) ──────────────────────────────────────────────────────────

const dashboardTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Alpha Network Explorer — Dashboard</title>
` + baseCSS + `
</head><body>
` + navHTML + `
<div class="container">
  <div class="page-title"><span>📊</span> Chain Dashboard</div>
  <div class="stat-grid" id="statGrid">
    <div class="stat-card"><div class="icon">🧱</div><div class="value" id="statHeight">—</div><div class="label">Block Height</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">⚡</div><div class="value" id="statBps">—</div><div class="label">Blocks / sec</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">📝</div><div class="value" id="statTxs">—</div><div class="label">Total Transactions</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">🤖</div><div class="value" id="statAgents">—</div><div class="label">Active Agents</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">💰</div><div class="value" id="statCirc">—</div><div class="label">Circulating $ALPHA</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">🔥</div><div class="value" id="statBurned">—</div><div class="label">Burned $ALPHA</div><div class="glow"></div></div>
  </div>

  <div class="card">
    <h2>Network Status</h2>
    <div class="info-grid">
      <div class="info-label">Chain ID</div><div class="info-value" id="infoChainID">—</div>
      <div class="info-label">Consensus</div><div class="info-value"><span class="badge badge-cyan" id="infoConsensus">—</span></div>
      <div class="info-label">Version</div><div class="info-value" id="infoVersion">—</div>
      <div class="info-label">Token</div><div class="info-value" id="infoToken">—</div>
      <div class="info-label">Total Supply</div><div class="info-value" id="infoSupply">—</div>
      <div class="info-label">Network</div><div class="info-value"><span class="badge badge-gold" id="infoStatus">—</span></div>
      <div class="info-label">Last Refresh</div><div class="info-value"><span class="ts" id="infoRefresh">—</span></div>
    </div>
  </div>
</div>
` + footerHTML + `
<script>
function fmt(n){if(!n&&n!==0)return'0';if(typeof n==='string')return n;if(n>=1e9)return(n/1e9).toFixed(2)+'B';if(n>=1e6)return(n/1e6).toFixed(2)+'M';if(n>=1e3)return(n/1e3).toFixed(1)+'K';return n.toString();}
function showLoading(){document.getElementById('loadingBar').classList.add('active')}
function hideLoading(){document.getElementById('loadingBar').classList.remove('active')}
function refreshDashboard(){
  showLoading();
  fetch('/api/chain-info').then(r=>r.json()).then(d=>{
    document.getElementById('statHeight').textContent=d.height||'0';
    document.getElementById('statBps').textContent=d.blocks_per_sec?(Number(d.blocks_per_sec).toFixed(2)):'0.00';
    document.getElementById('statTxs').textContent=d.tx_count?fmt(d.tx_count):'0';
    document.getElementById('statAgents').textContent=d.agent_count?fmt(d.agent_count):'0';
    document.getElementById('statCirc').textContent=d.circulating_supply!==undefined?fmt(d.circulating_supply):'0';
    document.getElementById('statBurned').textContent=d.total_burned!==undefined?fmt(d.total_burned):'0';
    document.getElementById('infoChainID').textContent=d.chain_id||'—';
    document.getElementById('infoConsensus').textContent=d.consensus||'—';
    document.getElementById('infoVersion').textContent=d.version||'—';
    document.getElementById('infoToken').textContent=d.token||'—';
    document.getElementById('infoSupply').textContent=d.total_supply!==undefined?fmt(d.total_supply)+' $ALPHA':'—';
    document.getElementById('infoStatus').textContent=d.status||'—';
    document.getElementById('infoRefresh').textContent=new Date().toLocaleTimeString()+' UTC';
    hideLoading();
  }).catch(()=>{hideLoading()});
}
window.addEventListener('load',refreshDashboard);
setInterval(refreshDashboard,5000);
</script>
</body></html>`

// ─── Blocks List (/blocks) ────────────────────────────────────────────────────

const blocksListTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Blocks — Alpha Network Explorer</title>
` + baseCSS + `
</head><body>
` + navHTML + `
<div class="container">
  <div class="page-title"><span>🧱</span> Block Explorer</div>
  <div class="search-bar">
    <input type="text" id="blockSearch" placeholder="Search by block height..." onkeydown="if(event.key==='Enter')searchBlock()">
    <button onclick="searchBlock()">Search</button>
  </div>
  <div class="card">
    <div class="card-title-row">
      <h2>Recent Blocks</h2>
      <span class="card-subtitle"><span class="loading-dot" id="blocksLoadingDot"></span></span>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th onclick="sortBlocks('height')">Height <span class="sort-arrow" id="sortHeight">▼</span></th><th>Hash</th><th onclick="sortBlocks('time')">Timestamp <span class="sort-arrow" id="sortTime">▼</span></th><th onclick="sortBlocks('txs')">Txs <span class="sort-arrow" id="sortTxs">▼</span></th><th>Validator</th></tr></thead>
        <tbody id="blocksBody"></tbody>
      </table>
    </div>
    <div class="pagination" id="blocksPagination"></div>
  </div>
</div>
` + footerHTML + `
<script>
var allBlocks=[],blocksPageSize=25,blocksPage=1,sortField='height',sortDir=-1;

function showLoading(){document.getElementById('loadingBar').classList.add('active')}
function hideLoading(){document.getElementById('loadingBar').classList.remove('active')}
function dotOn(){document.getElementById('blocksLoadingDot').classList.add('active')}
function dotOff(){document.getElementById('blocksLoadingDot').classList.remove('active')}

function searchBlock(){
  var v=document.getElementById('blockSearch').value.trim();
  if(!v)return;
  var h=parseInt(v);
  if(isNaN(h)||h<0)return;
  window.location.href='/blocks/'+h;
}

function formatHash(h){if(!h)return'—';return h.length>16?h.slice(0,16)+'…':h}
function formatTime(ts){if(!ts||ts==='—')return'—';var t=new Date(ts>1e12?ts:ts*1000);return t.toISOString().replace('T',' ').slice(0,19)+' UTC'}

function sortBlocks(field){
  if(sortField===field){sortDir*=-1}else{sortField=field;sortDir=-1}
  document.querySelectorAll('.sort-arrow').forEach(e=>e.classList.remove('active'));
  var el=document.getElementById('sort'+field.charAt(0).toUpperCase()+field.slice(1));
  if(el){el.classList.add('active');el.textContent=sortDir<0?'▲':'▼'}
  renderBlocksPage(1);
}

function renderBlocksPage(page){
  blocksPage=page;
  var sorted=[...allBlocks];
  sorted.sort((a,b)=>{
    var va, vb;
    if(sortField==='height'){va=Number(a.height)||0;vb=Number(b.height)||0}
    else if(sortField==='txs'){va=Number(a.tx_count)||0;vb=Number(b.tx_count)||0}
    else{va=a.timestamp||0;vb=b.timestamp||0}
    return va<vb?sortDir:va>vb?-sortDir:0;
  });
  var total=sorted.length,totalPages=Math.ceil(total/blocksPageSize)||1;
  if(page<1)page=1;if(page>totalPages)page=totalPages;
  var start=(page-1)*blocksPageSize,end=Math.min(start+blocksPageSize,total);
  var pageData=sorted.slice(start,end);
  var html='';
  pageData.forEach(function(b){
    var hashShort=formatHash(b.hash);
    var timeStr=formatTime(b.timestamp);
    var txs=b.tx_count||b.txCount||0;
    var val=b.validator_id||b.validator||'—';
    var prevHash=b.prev_hash||'—';
    var hashFull=b.hash||'—';
    var poi='';
    if(b.poi_proof){poi=JSON.stringify(b.poi_proof).slice(0,80)}
    html+='<tr class="clickable" onclick="toggleBlock(this,\''+b.height+'\')">'
      +'<td class="mono" style="color:#00F5FF;font-weight:600">#'+b.height+'</td>'
      +'<td><span class="hash-trunc">'+hashShort+'</span></td>'
      +'<td class="ts">'+timeStr+'</td>'
      +'<td>'+txs+'</td>'
      +'<td><span class="addr">'+val+'</span></td>'
      +'</tr>'
      +'<tr class="block-detail-row" id="detail-'+b.height+'">'
      +'<td colspan="5"><div class="block-detail-inner">'
      +'<span class="detail-label">Prev Hash</span><span class="detail-value">'+prevHash+'</span>'
      +'<span class="detail-label">Full Hash</span><span class="detail-value">'+hashFull+'</span>'
      +'<span class="detail-label">Transactions</span><span class="detail-value">'+txs+'</span>'
      +(poi?'<span class="detail-label">PoI Proof</span><span class="detail-value badge badge-green">✓ '+poi+'</span>':'')
      +'<span class="detail-label" style="margin-top:6px">🔗</span><span class="detail-value" style="margin-top:6px"><a href="/blocks/'+b.height+'" style="font-size:.78rem">View full block details →</a></span>'
      +'</div></td></tr>';
  });
  if(!pageData.length)html='<tr><td colspan="5" style="text-align:center;color:#5a6a7a;padding:30px">No blocks found</td></tr>';
  document.getElementById('blocksBody').innerHTML=html;
  var pgHtml='<button onclick="renderBlocksPage(1)" '+(page<=1?'disabled':'')+'>First</button>';
  pgHtml+='<button onclick="renderBlocksPage('+(page-1)+')" '+(page<=1?'disabled':'')+'>← Prev</button>';
  pgHtml+='<span class="page-info">Page '+page+' of '+totalPages+'</span>';
  pgHtml+='<button onclick="renderBlocksPage('+(page+1)+')" '+(page>=totalPages?'disabled':'')+'>Next →</button>';
  pgHtml+='<button onclick="renderBlocksPage('+totalPages+')" '+(page>=totalPages?'disabled':'')+'>Last</button>';
  document.getElementById('blocksPagination').innerHTML=pgHtml;
}

function toggleBlock(row,height){
  var detail=document.getElementById('detail-'+height);
  if(!detail)return;
  var expanded=row.classList.contains('expanded');
  document.querySelectorAll('.expanded').forEach(r=>r.classList.remove('expanded'));
  document.querySelectorAll('.block-detail-row').forEach(r=>r.style.display='none');
  if(!expanded){row.classList.add('expanded');detail.style.display='table-row'}
}

function refreshBlocks(){
  dotOn();
  fetch('/api/blocks').then(r=>r.json()).then(function(data){
    if(data.blocks){
      // Filter duplicates by height
      var seen={};
      data.blocks.forEach(function(b){
        if(!b.height)return;
        if(!seen[b.height]||b.height>seen[b.height])seen[b.height]=b;
      });
      allBlocks=Object.values(seen).sort((a,b)=>Number(b.height)-Number(a.height));
    }
    renderBlocksPage(blocksPage);
    dotOff();
  }).catch(function(){dotOff();renderBlocksPage(1)});
}

window.addEventListener('load',refreshBlocks);
setInterval(refreshBlocks,5000);
</script>
</body></html>`

// ─── Block Detail (/blocks/{height}) ─────────────────────────────────────────

const blockDetailTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Block #{{.Height}} — Alpha Network Explorer</title>
` + baseCSS + `
</head><body>
` + navHTML + `
<div class="container">
  <div class="page-title"><span>🧱</span> Block #{{.Height}}</div>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="card">
    <h2>Block Summary</h2>
    <div class="info-grid">
      <div class="info-label">Height</div><div class="info-value" style="color:#00F5FF;font-weight:600">#{{.Height}}</div>
      <div class="info-label">Timestamp</div><div class="info-value"><span class="ts">{{.TimeFmt}}</span></div>
      <div class="info-label">Validator</div><div class="info-value"><span class="addr">{{.Validator}}</span></div>
      <div class="info-label">Prev Hash</div><div class="info-value"><span class="hash-full">{{.PrevHash}}</span></div>
      <div class="info-label">Hash</div><div class="info-value"><span class="hash-full">{{.Hash}}</span></div>
      <div class="info-label">Transactions</div><div class="info-value">{{.TxCount}}</div>
      {{if .PoISummary}}</span><div class="info-label">PoI Proof</div><div class="info-value"><span class="badge badge-green">✓ {{.PoISummary}}</div>{{end}}
    </div>
  </div>

  <div class="card">
    <h2>Transactions ({{.TxCount}})</h2>
    <div class="table-wrap">
      <table>
        <thead><tr><th>Tx ID</th><th>Type</th><th>From</th><th>To</th><th>Amount</th></tr></thead>
        <tbody>
        {{range .Txs}}
        <tr>
          <td><span class="hash-trunc">{{.TxID}}</span></td>
          <td><span class="badge badge-blue">{{.Type}}</span></td>
          <td><span class="addr">{{.From}}</span></td>
          <td><span class="addr">{{.To}}</span></td>
          <td class="mono">{{.Amount}}</td>
        </tr>
        {{else}}<tr><td colspan="5" style="text-align:center;color:#5a6a7a;padding:30px">No transactions in this block</td></tr>{{end}}
        </tbody>
      </table>
    </div>
  </div>
  {{end}}
</div>
` + footerHTML + `
</body></html>`

// ─── Agents Leaderboard (/agents) ─────────────────────────────────────────────

const agentsTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Agents — Alpha Network Explorer</title>
` + baseCSS + `
</head><body>
` + navHTML + `
<div class="container">
  <div class="page-title"><span>🤖</span> Agent Leaderboard</div>
  <div class="card">
    <div class="card-title-row">
      <h2>All Agents</h2>
      <span class="card-subtitle"><span class="loading-dot" id="agentsLoadingDot"></span></span>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th onclick="sortAgents('rank')">Rank <span class="sort-arrow" id="sortRank">▼</span></th><th onclick="sortAgents('id')">Agent ID <span class="sort-arrow" id="sortId">▼</span></th><th onclick="sortAgents('stake')">Stake <span class="sort-arrow" id="sortStake">▼</span></th><th onclick="sortAgents('rep')">Reputation <span class="sort-arrow" id="sortRep">▼</span></th><th>Tier</th><th>Status</th></tr></thead>
        <tbody id="agentsBody"></tbody>
      </table>
    </div>
    <div class="pagination" id="agentsPagination"></div>
  </div>
</div>
` + footerHTML + `
<script>
var allAgents=[],agentsPageSize=25,agentsPage=1,agentSortField='rank',agentSortDir=-1;

function showLoading(){document.getElementById('loadingBar').classList.add('active')}
function hideLoading(){document.getElementById('loadingBar').classList.remove('active')}
function dotOn(){document.getElementById('agentsLoadingDot').classList.add('active')}
function dotOff(){document.getElementById('agentsLoadingDot').classList.remove('active')}

function getTierBadge(tier){
  if(!tier)return '<span class="badge tier-seed">Seed</span>';
  var t=tier.toLowerCase();
  if(t==='elite')return '<span class="badge tier-elite">Elite</span>';
  if(t==='trusted')return '<span class="badge tier-trusted">Trusted</span>';
  if(t==='active')return '<span class="badge tier-active">Active</span>';
  return '<span class="badge tier-seed">Seed</span>';
}

function getStatusBadge(status){
  if(!status) return '<span class="badge badge-gray">—</span>';
  switch(String(status).toLowerCase()){
    case 'active':     return '<span class="badge badge-green">🟢 Active</span>';
    case 'hibernated': return '<span class="badge badge-blue">💤 Hibernated</span>';
    case 'unresponsive': return '<span class="badge badge-orange">⚠️ Unresponsive</span>';
    case 'dead':       return '<span class="badge badge-red">💀 Dead</span>';
    default:           return '<span class="badge badge-gray">'+status+'</span>';
  }
}

function sortAgents(field){
  if(agentSortField===field){agentSortDir*=-1}else{agentSortField=field;agentSortDir=-1}
  document.querySelectorAll('.sort-arrow').forEach(e=>e.classList.remove('active'));
  var idMap={rank:'Rank',id:'Id',stake:'Stake',rep:'Rep'};
  var el=document.getElementById('sort'+idMap[field]);
  if(el){el.classList.add('active');el.textContent=agentSortDir<0?'▲':'▼'}
  renderAgentsPage(1);
}

function renderAgentsPage(page){
  agentsPage=page;
  var sorted=[...allAgents];
  sorted.sort(function(a,b){
    var va,vb;
    if(agentSortField==='rank'){va=a.rank||0;vb=b.rank||0}
    else if(agentSortField==='id'){va=(a.agent_id||a.id||'').toLowerCase();vb=(b.agent_id||b.id||'').toLowerCase();return va<vb?agentSortDir:va>vb?-agentSortDir:0}
    else if(agentSortField==='stake'){va=Number(a.stake)||0;vb=Number(b.stake)||0}
    else{va=Number(a.reputation_score||a.reputation)||0;vb=Number(b.reputation_score||b.reputation)||0}
    return va<vb?agentSortDir:va>vb?-agentSortDir:0;
  });
  var total=sorted.length,totalPages=Math.ceil(total/agentsPageSize)||1;
  if(page<1)page=1;if(page>totalPages)page=totalPages;
  var start=(page-1)*agentsPageSize,end=Math.min(start+agentsPageSize,total);
  var pageData=sorted.slice(start,end);
  var html='';
  pageData.forEach(function(a,i){
    var rank=start+i+1;
    var id=a.agent_id||a.id||'—';
    var stake=a.stake?fmt(Number(a.stake)):'0';
    var rep=a.reputation_score||a.reputation||'—';
    var tier=a.tier||a.trust_tier||'Seed';
    var active=a.status||a.active;
    html+='<tr>'
      +'<td class="mono" style="color:#5a7a9a">'+(rank<=3?'🥇🥈🥉'[rank-1]:rank)+'</td>'
      +'<td><a href="/agents/'+id+'"><span class="hash-trunc" style="max-width:200px">'+id+'</span></a></td>'
      +'<td class="mono">'+stake+'</td>'
      +'<td class="mono" style="color:'+(rep>=80?'#00cc66':rep>=50?'#fc0':'#8c96a0')+'">'+rep+'</td>'
      +'<td>'+getTierBadge(tier)+'</td>'
      +'<td>'+getStatusBadge(active)+'</td>'
      +'</tr>';
  });
  if(!pageData.length)html='<tr><td colspan="6" style="text-align:center;color:#5a6a7a;padding:30px">No agents registered</td></tr>';
  document.getElementById('agentsBody').innerHTML=html;
  var pgHtml='<button onclick="renderAgentsPage(1)" '+(page<=1?'disabled':'')+'>First</button>';
  pgHtml+='<button onclick="renderAgentsPage('+(page-1)+')" '+(page<=1?'disabled':'')+'>← Prev</button>';
  pgHtml+='<span class="page-info">Page '+page+' of '+totalPages+'</span>';
  pgHtml+='<button onclick="renderAgentsPage('+(page+1)+')" '+(page>=totalPages?'disabled':'')+'>Next →</button>';
  pgHtml+='<button onclick="renderAgentsPage('+totalPages+')" '+(page>=totalPages?'disabled':'')+'>Last</button>';
  document.getElementById('agentsPagination').innerHTML=pgHtml;
}

function fmt(n){if(!n&&n!==0)return'0';if(n>=1e9)return(n/1e9).toFixed(2)+'B';if(n>=1e6)return(n/1e6).toFixed(2)+'M';if(n>=1e3)return(n/1e3).toFixed(1)+'K';return n.toString();}

function refreshAgents(){
  dotOn();
  fetch('/api/agents').then(r=>r.json()).then(function(data){
    allAgents=data.agents||[];
    renderAgentsPage(agentsPage);
    dotOff();
  }).catch(function(){dotOff();renderAgentsPage(1)});
}

window.addEventListener('load',refreshAgents);
setInterval(refreshAgents,5000);
</script>
</body></html>`

// ─── Agent Profile (/agents/{address}) ────────────────────────────────────────

const agentDetailTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Agent {{.AgentID}} — Alpha Explorer</title>
` + baseCSS + `
</head><body>
` + navHTML + `
<div class="container">
  <div class="page-title"><span>🤖</span> Agent Profile</div>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="card">
    <h2>Identity</h2>
    <div class="info-grid">
      <div class="info-label">Agent ID</div><div class="info-value">{{.AgentID}}</div>
      <div class="info-label">Address</div><div class="info-value"><span class="hash-full">{{.Address}}</span></div>
      <div class="info-label">Reputation</div><div class="info-value" style="color:{{if ge .Reputation 80}}#00cc66{{else if ge .Reputation 50}}#fc0{{else}}#8c96a0{{end}};font-weight:600">{{.Reputation}}</div>
      <div class="info-label">Tier</div><div class="info-value">{{.Tier}}</div>
      <div class="info-label">Trust Score</div><div class="info-value" style="color:#a6f">{{printf "%.4f" .TrustScore}}</div>
      <div class="info-label">Status</div><div class="info-value"><span class="badge {{.StatusClass}}">{{.Status}}</span></div>
    </div>
  </div>
  <div class="card">
    <h2>Economics</h2>
    <div class="info-grid">
      <div class="info-label">Stake</div><div class="info-value" style="color:#00F5FF">{{.Stake}} $ALPHA</div>
      <div class="info-label">Balance</div><div class="info-value">{{.Balance}} $ALPHA</div>
      <div class="info-label">Tasks Completed</div><div class="info-value">{{.Tasks}}</div>
    </div>
  </div>
  <div class="card">
    <h2>Blockchain</h2>
    <div class="info-grid">
      <div class="info-label">Created at Block</div><div class="info-value">#{{.CreatedBlock}}</div>
      <div class="info-label">Last Active Block</div><div class="info-value">#{{.LastActive}}</div>
    </div>
  </div>
  <div class="card">
    <h2>Capabilities</h2>
    <div style="display:flex;flex-wrap:wrap;gap:6px">
      {{range .Caps}}<span class="badge badge-cyan">{{.}}</span>{{end}}
      {{if not .Caps}}<span class="ts">No capabilities listed</span>{{end}}
    </div>
  </div>
  {{end}}
</div>
` + footerHTML + `
</body></html>`

// ─── Tasks (/tasks) ───────────────────────────────────────────────────────────

const tasksTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Tasks — Alpha Network Explorer</title>
` + baseCSS + `
</head><body>
` + navHTML + `
<div class="container">
  <div class="page-title"><span>📋</span> Task Marketplace</div>
  <div class="card">
    <div class="card-title-row">
      <h2>All Tasks</h2>
      <span class="card-subtitle"><span class="loading-dot" id="tasksLoadingDot"></span></span>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th>Task ID</th><th>Capability</th><th>Status</th><th>Reward</th><th>Posted By</th><th>Deadline</th></tr></thead>
        <tbody id="tasksBody"></tbody>
      </table>
    </div>
    <div class="pagination" id="tasksPagination"></div>
  </div>
</div>
` + footerHTML + `
<script>
var allTasks=[],tasksPageSize=25,tasksPage=1;

function showLoading(){document.getElementById('loadingBar').classList.add('active')}
function hideLoading(){document.getElementById('loadingBar').classList.remove('active')}
function dotOn(){document.getElementById('tasksLoadingDot').classList.add('active')}
function dotOff(){document.getElementById('tasksLoadingDot').classList.remove('active')}

function getStatusBadge(status){
  if(!status)return'<span class="badge badge-gray">Unknown</span>';
  var s=status.toLowerCase();
  if(s==='pending'||s==='open')return'<span class="badge badge-gold">Pending</span>';
  if(s==='assigned'||s==='in_progress')return'<span class="badge badge-blue">Assigned</span>';
  if(s==='completed'||s==='done')return'<span class="badge badge-green">Completed</span>';
  if(s==='failed')return'<span class="badge badge-red">Failed</span>';
  return'<span class="badge badge-gray">'+status+'</span>';
}

function fmt(n){if(!n&&n!==0)return'0';if(typeof n==='string')return n;if(n>=1e9)return(n/1e9).toFixed(2)+'B';if(n>=1e6)return(n/1e6).toFixed(2)+'M';if(n>=1e3)return(n/1e3).toFixed(1)+'K';return n.toString();}

function formatTime(ts){if(!ts||ts==='—'||ts===0)return'—';var t=new Date(ts>1e12?ts:ts*1000);return t.toISOString().replace('T',' ').slice(0,19)+' UTC'}

function renderTasksPage(page){
  tasksPage=page;
  var total=allTasks.length,totalPages=Math.ceil(total/tasksPageSize)||1;
  if(page<1)page=1;if(page>totalPages)page=totalPages;
  var start=(page-1)*tasksPageSize,end=Math.min(start+tasksPageSize,total);
  var pageData=allTasks.slice(start,end);
  var html='';
  pageData.forEach(function(t){
    var id=t.task_id||'—';
    var cap=t.capability||'—';
    var reward=t.reward||t.reward_amount||0;
    var status=t.status||'pending';
    var posted=t.posted_by||t.poster||'—';
    var deadline=t.deadline||t.due_date||0;
    html+='<tr>'
      +'<td><span class="hash-trunc">'+id+'</span></td>'
      +'<td><span class="badge badge-cyan">'+cap+'</span></td>'
      +'<td>'+getStatusBadge(status)+'</td>'
      +'<td class="mono">'+fmt(Number(reward))+' $ALPHA</td>'
      +'<td><span class="addr">'+posted+'</span></td>'
      +'<td class="ts">'+formatTime(deadline)+'</td>'
      +'</tr>';
  });
  if(!pageData.length)html='<tr><td colspan="6" style="text-align:center;color:#5a6a7a;padding:30px">No tasks found</td></tr>';
  document.getElementById('tasksBody').innerHTML=html;
  var pgHtml='<button onclick="renderTasksPage(1)" '+(page<=1?'disabled':'')+'>First</button>';
  pgHtml+='<button onclick="renderTasksPage('+(page-1)+')" '+(page<=1?'disabled':'')+'>← Prev</button>';
  pgHtml+='<span class="page-info">Page '+page+' of '+totalPages+'</span>';
  pgHtml+='<button onclick="renderTasksPage('+(page+1)+')" '+(page>=totalPages?'disabled':'')+'>Next →</button>';
  pgHtml+='<button onclick="renderTasksPage('+totalPages+')" '+(page>=totalPages?'disabled':'')+'>Last</button>';
  document.getElementById('tasksPagination').innerHTML=pgHtml;
}

function refreshTasks(){
  dotOn();
  fetch('/api/tasks').then(r=>r.json()).then(function(data){
    allTasks=data.tasks||[];
    renderTasksPage(tasksPage);
    dotOff();
  }).catch(function(){dotOff();renderTasksPage(1)});
}

window.addEventListener('load',refreshTasks);
setInterval(refreshTasks,5000);
</script>
</body></html>`

// ─── Intelligence Oracle (/intelligence) ─────────────────────────────────────

const intelligenceTmpl = `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Intelligence Oracle — Alpha Network Explorer</title>
` + baseCSS + `
</head><body>
` + navHTML + `
<div class="container">
  <div class="page-title"><span>🧠</span> Intelligence Oracle</div>
  {{if .Error}}<div class="err">{{.Error}}</div>{{else}}
  <div class="stat-grid">
    <div class="stat-card"><div class="icon">🤖</div><div class="value" id="intelTotalAgents">{{.TotalAgents}}</div><div class="label">Total Agents</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">💾</div><div class="value" id="intelDataPoints">{{.TotalDataPoints}}</div><div class="label">Data Points</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">⏱️</div><div class="value" id="intelLatency">{{printf "%.0f" .AvgLatency}}ms</div><div class="label">Avg Latency</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">✅</div><div class="value" id="intelConsensus">{{printf "%.1f" .ConsensusRate}}%</div><div class="label">Consensus Rate</div><div class="glow"></div></div>
  </div>
  <div class="stat-grid">
    <div class="stat-card"><div class="icon">📊</div><div class="value" id="intelEntropy">{{printf "%.4f" .AvgEntropy}}</div><div class="label">Avg Output Entropy</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">🏆</div><div class="value" id="intelScore">{{printf "%.4f" .NetworkScore}}</div><div class="label">Network Intelligence Score</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">👤</div><div class="value" id="intelUnique">{{.UniqueAgents}}</div><div class="label">Unique Agents</div><div class="glow"></div></div>
    <div class="stat-card"><div class="icon">📈</div><div class="value" id="intelUpdated">—</div><div class="label">Last Updated</div><div class="glow"></div></div>
  </div>

  <div class="card">
    <div class="card-title-row">
      <h2>Top Agents Leaderboard</h2>
      <span class="card-subtitle"><span class="loading-dot" id="intelLoadingDot"></span></span>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th>Rank</th><th>Agent ID</th><th>Reputation</th><th>Tasks</th><th>Avg Latency</th><th>Consensus Rate</th></tr></thead>
        <tbody id="intelTopBody">
          <tr><td colspan="6" style="text-align:center;color:#5a6a7a;padding:30px" id="intelTopLoading">Loading...</td></tr>
        </tbody>
      </table>
    </div>
    <div class="pagination" id="intelPagination"></div>
  </div>
</div>
{{end}}
` + footerHTML + `
<script>
var intelTopAgents=[],intelPageSize=10,intelPage=1;

function showLoading(){document.getElementById('loadingBar').classList.add('active')}
function hideLoading(){document.getElementById('loadingBar').classList.remove('active')}

function refreshIntelligence(){
  // Refresh stats
  fetch('/api/intelligence').then(r=>r.json()).then(function(d){
    if(d.total_agents!=null)document.getElementById('intelTotalAgents').textContent=d.total_agents;
    if(d.total_data_points!=null)document.getElementById('intelDataPoints').textContent=d.total_data_points;
    if(d.avg_latency!=null)document.getElementById('intelLatency').textContent=Number(d.avg_latency).toFixed(0)+'ms';
    if(d.consensus_rate!=null)document.getElementById('intelConsensus').textContent=Number(d.consensus_rate).toFixed(1)+'%';
    if(d.avg_entropy!=null)document.getElementById('intelEntropy').textContent=Number(d.avg_entropy).toFixed(4);
    if(d.network_score!=null)document.getElementById('intelScore').textContent=Number(d.network_score).toFixed(4);
    if(d.unique_agents!=null)document.getElementById('intelUnique').textContent=d.unique_agents;
    document.getElementById('intelUpdated').textContent=new Date().toLocaleTimeString();
  }).catch(function(){});

  // Refresh top agents
  var dot=document.getElementById('intelLoadingDot');
  if(dot)dot.classList.add('active');
  fetch('/api/top-agents').then(r=>r.json()).then(function(data){
    intelTopAgents=data.agents||[];
    renderIntelTop(intelPage);
    if(dot)dot.classList.remove('active');
  }).catch(function(){if(dot)dot.classList.remove('active')});
}

function renderIntelTop(page){
  intelPage=page;
  var total=intelTopAgents.length,totalPages=Math.ceil(total/intelPageSize)||1;
  if(page<1)page=1;if(page>totalPages)page=totalPages;
  var start=(page-1)*intelPageSize,end=Math.min(start+intelPageSize,total);
  var pageData=intelTopAgents.slice(start,end);
  var html='';
  pageData.forEach(function(a,i){
    var rank=start+i+1;
    var id=a.agent_id||a.id||'—';
    var rep=a.reputation_score||a.reputation||'—';
    var tasks=a.task_count||a.tasks||'—';
    var lat=a.avg_latency||a.latency||'—';
    var cr=a.consensus_rate||'—';
    html+='<tr>'
      +'<td class="mono" style="color:#5a7a9a">'+(rank<=3?'🥇🥈🥉'[rank-1]:rank)+'</td>'
      +'<td><a href="/agents/'+id+'"><span class="hash-trunc" style="max-width:200px">'+id+'</span></a></td>'
      +'<td class="mono" style="color:'+(rep>=80?'#00cc66':rep>=50?'#fc0':'#8c96a0')+'">'+rep+'</td>'
      +'<td class="mono">'+tasks+'</td>'
      +'<td class="mono">'+(lat!=='—'?Number(lat).toFixed(0)+'ms':'—')+'</td>'
      +'<td class="mono">'+(cr!=='—'?Number(cr).toFixed(1)+'%':'—')+'</td>'
      +'</tr>';
  });
  if(!pageData.length)html='<tr><td colspan="6" style="text-align:center;color:#5a6a7a;padding:30px">No agent data available</td></tr>';
  document.getElementById('intelTopBody').innerHTML=html;
  var pgHtml='<button onclick="renderIntelTop(1)" '+(page<=1?'disabled':'')+'>First</button>';
  pgHtml+='<button onclick="renderIntelTop('+(page-1)+')" '+(page<=1?'disabled':'')+'>← Prev</button>';
  pgHtml+='<span class="page-info">Page '+page+' of '+totalPages+'</span>';
  pgHtml+='<button onclick="renderIntelTop('+(page+1)+')" '+(page>=totalPages?'disabled':'')+'>Next →</button>';
  pgHtml+='<button onclick="renderIntelTop('+totalPages+')" '+(page>=totalPages?'disabled':'')+'>Last</button>';
  document.getElementById('intelPagination').innerHTML=pgHtml;
}

window.addEventListener('load',refreshIntelligence);
setInterval(refreshIntelligence,5000);
</script>
</body></html>`

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

// ─── API Proxy Handlers (JSON for JS frontend) ───────────────────────────────

// GET /api/chain-info — proxy to /api/v1/chain/info
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

// GET /api/blocks — returns last 30 blocks
func handleAPIBlocks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	info, err := apiGet("/api/v1/chain/info")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"%s","blocks":[]}`, err.Error())
		return
	}

	topHeight := safeInt(info, "height")
	limit := int64(30)
	if topHeight <= 0 {
		fmt.Fprintf(w, `{"blocks":[],"count":0}`)
		return
	}

	var blocks []interface{}
	for i := topHeight; i > topHeight-limit && i >= 0; i-- {
		blk, err := apiGet(fmt.Sprintf("/api/v1/blocks/%d", i))
		if err != nil {
			continue
		}
		if bm, ok := blk["block"].(map[string]interface{}); ok && bm != nil {
			blocks = append(blocks, bm)
		} else {
			blocks = append(blocks, blk)
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"blocks": blocks,
		"count":  len(blocks),
	})
}

// GET /api/agents — returns agent list
func handleAPIAgents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	agents, err := apiGetSlice("/api/v1/agents?limit=50", "agents")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"%s","agents":[]}`, err.Error())
		return
	}

	// Enrich each agent with tier and rank info
	type enrichedAgent struct {
		AgentID   string      `json:"agent_id"`
		Address   string      `json:"address"`
		Stake     interface{} `json:"stake"`
		Reputation interface{} `json:"reputation_score"`
		Tier      string      `json:"tier"`
		Status    string      `json:"status"`
		Tasks     interface{} `json:"task_count"`
		Caps      []string    `json:"capabilities"`
	}

	var result []*enrichedAgent
	for _, a := range agents {
		am, _ := a.(map[string]interface{})
		if am == nil {
			continue
		}
		rep := safeInt(am, "reputation_score")
		stake := safeInt(am, "stake")

		tier := "Seed"
		switch {
		case stake >= 100000:
			tier = "Elite"
		case stake >= 10000:
			tier = "Trusted"
		case stake >= 1000:
			tier = "Active"
		}

		status := "Active"
		if rep < 10 {
			status = "Inactive"
		}

		var caps []string
		if capRaw, ok := am["capabilities"].([]interface{}); ok {
			for _, c := range capRaw {
				caps = append(caps, fmt.Sprintf("%v", c))
			}
		}

		result = append(result, &enrichedAgent{
			AgentID:    safeStr(am, "agent_id"),
			Address:    safeStr(am, "address"),
			Stake:      stake,
			Reputation: rep,
			Tier:       tier,
			Status:     status,
			Tasks:      safeInt(am, "task_count"),
			Caps:       caps,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"agents": result,
		"count":  len(result),
	})
}

// GET /api/tasks — returns task list
func handleAPITasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Try the v1 tasks endpoint
	tasks, err := apiGetSlice("/api/v1/tasks", "tasks")
	if err != nil {
		// Fallback: try /api/v1/tasks/available
		tasks, err = apiGetSlice("/api/v1/tasks/available", "tasks")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, `{"error":"%s","tasks":[]}`, err.Error())
			return
		}
	}

	type taskRow struct {
		TaskID     string      `json:"task_id"`
		Capability string      `json:"capability"`
		Reward     interface{} `json:"reward"`
		Status     string      `json:"status"`
		PostedBy   string      `json:"posted_by"`
		Deadline   int64       `json:"deadline"`
	}

	var result []*taskRow
	for _, t := range tasks {
		tm, _ := t.(map[string]interface{})
		if tm == nil {
			continue
		}
		deadline := safeInt(tm, "deadline")
		result = append(result, &taskRow{
			TaskID:     safeStr(tm, "task_id"),
			Capability: safeStr(tm, "capability"),
			Reward:     safeInt(tm, "reward"),
			Status:     safeStr(tm, "status"),
			PostedBy:   safeStr(tm, "posted_by"),
			Deadline:   deadline,
		})
	}

	// Also fetch marketplace stats if available
	stats := map[string]interface{}{}
	if data, err := apiGet("/api/v1/tasks"); err == nil {
		if s, ok := data["stats"]; ok {
			stats = s.(map[string]interface{})
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": result,
		"count": len(result),
		"stats": stats,
	})
}

// GET /api/intelligence — combined intelligence stats
func handleAPIIntelligence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	stats, err := apiGet("/api/v1/intelligence/stats")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"%s"}`, err.Error())
		return
	}

	sm, _ := stats["stats"].(map[string]interface{})
	if sm == nil {
		sm = stats
	}

	// Get agent count
	info, _ := apiGet("/api/v1/chain/info")
	agentCount := safeInt(info, "agent_count")

	resp := map[string]interface{}{
		"total_agents":     agentCount,
		"total_data_points": safeInt(sm, "total_data_points"),
		"unique_agents":     safeInt(sm, "unique_agents"),
		"avg_latency":       safeFloat(sm, "avg_latency_ms"),
		"consensus_rate":    safeFloat(sm, "consensus_rate") * 100,
		"avg_entropy":       safeFloat(sm, "avg_output_entropy"),
		"network_score":     safeFloat(sm, "network_intelligence_score"),
	}

	json.NewEncoder(w).Encode(resp)
}

// GET /api/top-agents — top agents from intelligence oracle
func handleAPITopAgents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Try intelligence oracle first
	topData, err := apiGet("/api/v1/intelligence/top?limit=50")
	if err == nil {
		if agents, ok := topData["agents"].([]interface{}); ok && len(agents) > 0 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"agents": agents,
				"count":  len(agents),
			})
			return
		}
	}

	// Fallback: use agents list
	agents, err := apiGetSlice("/api/v1/agents?limit=50", "agents")
	if err != nil {
		fmt.Fprintf(w, `{"agents":[],"count":0}`)
		return
	}

	// Sort by reputation descending
	type agentScore struct {
		AgentID         string
		ReputationScore int64
		TaskCount       int64
		AvgLatency      float64
		ConsensusRate   float64
		Raw             map[string]interface{}
	}

	var scored []agentScore
	for _, a := range agents {
		am, _ := a.(map[string]interface{})
		if am == nil {
			continue
		}
		scored = append(scored, agentScore{
			AgentID:         safeStr(am, "agent_id"),
			ReputationScore: safeInt(am, "reputation_score"),
			TaskCount:       safeInt(am, "task_count"),
			AvgLatency:      safeFloat(am, "avg_latency_ms"),
			ConsensusRate:   safeFloat(am, "consensus_agreement_rate"),
			Raw:             am,
		})
	}

	// Sort descending by reputation
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].ReputationScore > scored[i].ReputationScore {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// Limit to top 50
	limit := 50
	if len(scored) > limit {
		scored = scored[:limit]
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"agents": scored,
		"count":  len(scored),
	})
}

// ─── Page Handlers ────────────────────────────────────────────────────────────

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
		Page        string
		AgentID     string
		Address     string
		Reputation  int64
		Stake       int64
		Tasks       int64
		TrustScore  float64
		Balance     int64
		CreatedBlock int64
		LastActive  int64
		Status      string
		StatusClass string
		Tier        string
		Caps        []string
		Error       string
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

	// Read actual agent status from the API response
	switch safeStr(id, "status") {
	case "active":
		d.Status = "🟢 Active"
		d.StatusClass = "badge-green"
	case "hibernated":
		d.Status = "💤 Hibernated"
		d.StatusClass = "badge-blue"
	case "unresponsive":
		d.Status = "⚠️ Unresponsive"
		d.StatusClass = "badge-orange"
	case "dead":
		d.Status = "💀 Dead"
		d.StatusClass = "badge-red"
	default:
		d.Status = "⚪ Unknown"
		d.StatusClass = "badge-gray"
	}

	// Read missed blocks for additional context
	if missed := safeInt(id, "missed_blocks"); missed > 0 {
		d.Status += fmt.Sprintf(" (%d missed)", missed)
	}
	stake := d.Stake
	switch {
	case stake >= 100000:
		d.Tier = `<span class="badge tier-elite">Elite</span>`
	case stake >= 10000:
		d.Tier = `<span class="badge tier-trusted">Trusted</span>`
	case stake >= 1000:
		d.Tier = `<span class="badge tier-active">Active</span>`
	default:
		d.Tier = `<span class="badge tier-seed">Seed</span>`
	}
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
			statusClass = "badge-gold"
		case "failed":
			statusClass = "badge-red"
		case "assigned":
			statusClass = "badge-blue"
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

	// API proxy for JS frontend
	mux.HandleFunc("/api/chain-info", handleAPIProxy)
	mux.HandleFunc("/api/blocks", handleAPIBlocks)
	mux.HandleFunc("/api/agents", handleAPIAgents)
	mux.HandleFunc("/api/tasks", handleAPITasks)
	mux.HandleFunc("/api/intelligence", handleAPIIntelligence)
	mux.HandleFunc("/api/top-agents", handleAPITopAgents)

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
