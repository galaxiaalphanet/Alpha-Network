# Alpha Network Security Audit — Self-Assessment
> Date: 2026-05-18 | Scope: v0.3.1 | Auditor: Alpha Network Team
> Status: **4/4 CRITICAL FIXES APPLIED & VERIFIED** ✅
> Files reviewed: `consensus/poi.go`, `ledger/ledger.go`, `api/server.go`

---

## 1. Consensus (`consensus/poi.go`)

### 1.1 ✅ RESOLVED — Commitment Verification is Now Operational
**Location:** `verifyCommitment()` in `consensus/poi.go`

**Fix (2026-05-18):** The `expected` hash is now compared against `proof.CommitmentHash`:
```go
return expected == proof.CommitmentHash
```
The ZK commitment scheme is now cryptographically enforced. Pre-image verification
is active: only proofs where RevealProof + AgentID hashes to CommitmentHash pass.

### 1.2 ⚠️ MEDIUM — `RunConsensus` Depletes Pending Proofs on Failure
**Location:** `RunConsensus()` lines 195-196
```go
// ... earlier quorum check returns error, THEN:
delete(e.pendingProofs, blockHeight)  // ← deletes even on early-return errors
```
**Issue:** When quorum is not met, `RunConsensus` returns an error early (line 117), but `delete(e.pendingProofs, blockHeight)` at line 196 still executes because the early return happens BEFORE the defer. Wait — looking again:

Actually, looking at the code flow: the quorum check at ~line 117 returns an error BEFORE the lock is released... but the lock is held via `defer e.mu.Unlock()`, and `delete(e.pendingProofs, blockHeight)` is at line ~196 BEFORE the return at line ~208. So on quorum failure, the proofs ARE deleted.

**Risk:** Honest validators who submitted proofs lose them when quorum fails. A temporary network partition can permanently drop valid proofs.

**Fix:** Only delete proofs on successful consensus. Move the `delete` into the success path, or use a deferred cleanup that only fires on success.

### 1.3 ✅ LOW — `isLatencyRealistic` Bounds Are Generous
Range 50ms–15000ms is reasonable for AI inference. No immediate risk but consider tightening to 100ms–10000ms for production.

### 1.4 ✅ LOW — VRF Selection Uses SHA256 Chaining
The `vrfSelect` function uses deterministic SHA256 chaining — this is fine for a single-node setup. For production with multiple validators, this should use a VRF with public verifiability (e.g., ECVRF).

### 1.5 ℹ️ INFO — Slashing Does Not Check Balance
In `RunConsensus`, slashes are computed as `float64(v.Agent.Stake) * core.SlashPenalty` but never checked against the agent's actual ledger balance before being applied in `producer.go`. The `Debit` in the producer will fail silently (`_ = p.ledger.Debit(...)`), meaning an agent with no balance cannot actually be slashed.

---

## 2. Ledger (`ledger/ledger.go`)

### 2.1 ✅ PASS — Thread Safety
All public methods properly acquire `l.mu.Lock()` or `l.mu.RLock()`. No data races detected.

### 2.2 ✅ PASS — Overdraft Protection
`Debit`, `Transfer`, and `BurnSupply` all check `bal < amount` before modifying balances.

### 2.3 ✅ PASS — Self-Transfer Prevention
`Transfer()` rejects `from == to` with a clear error.

### 2.4 ✅ PASS — Positive Amount Validation
All public methods reject amounts ≤ 0.

### 2.5 ⚠️ LOW — No Transaction Replay Protection
`genTxID` uses `time.Now().UnixNano()` as a nonce component, which is unique per call. However, `Transfer` generates its own TxID; there's no mechanism to prevent an attacker from submitting the same transfer twice if they bypass the API.

**Risk:** Low. The HTTP API generates unique TxIDs per request. An attacker would need direct mempool access.

**Fix:** Consider adding a nonce-per-address counter.

### 2.6 ⚠️ LOW — `BurnFromProtocol` Reduces `totalSupply`
```go
func (l *Ledger) BurnFromProtocol(amount core.Amount) error {
    l.totalBurned += amount
    l.totalSupply -= amount  // ← mutates total supply
```
**Issue:** `totalSupply` is conceptually the fixed 1B cap but `BurnFromProtocol` reduces it. This is intentional (deflationary), but `BurnSupply` does NOT reduce `totalSupply` — it only increments `totalBurned`. These two burn methods have inconsistent semantics.

**Risk:** Accounting confusion. `CirculatingSupply()` returns `totalSupply - totalBurned`, which double-counts burns from `BurnFromProtocol`.

**Fix:** Make `BurnSupply` also reduce `totalSupply`, or make `BurnFromProtocol` only increment `totalBurned`. Pick one consistent model.

### 2.7 ✅ PASS — Persistence Callbacks Are Best-Effort
`saveBalance` and `saveMeta` log errors rather than crashing. Correct behavior for a blockchain — persistence failures shouldn't halt the ledger.

### 2.8 ℹ️ INFO — No Upper Bound on `totalSupply`
The `NewLedger` constructor accepts any `totalSupply` value, including negative numbers (though `Amount` is likely unsigned). No validation that supply matches genesis config.

---

## 3. API Server (`api/server.go`)

### 3.1 ✅ RESOLVED — Input Size Limits Added to All Handlers
**Fix (2026-05-18):** Added `limitBody(w, r)` helper using `http.MaxBytesReader(w, r.Body, 1<<20)`.
Applied before every `json.NewDecoder(r.Body).Decode(...)` call (13 handlers).
Payloads > 1MB are rejected before JSON parsing begins, preventing memory-exhaustion DoS.
Verified: 2MB payload correctly rejected.

### 3.2 ✅ PASS — Method Checks on State-Changing Endpoints
All POST endpoints verify `r.Method == http.MethodPost`. Read endpoints accept GET. Correct.

### 3.3 ✅ RESOLVED — Agent Registration Now Enforces Sybil Protection
**Fix (2026-05-18):** `handleAgentRegister` now enforces exponential stake requirements:
- Agent 1: 1,000 $ALPHA | Agent 2: 10,000 | Agent 3: 100,000 (10x each)
- `core.RequiredStake(agentNumber)` calculates the tiered minimum
- Registration verifies the address actually holds the required stake in the ledger
- Stake is debited from the registrant's address (locking funds), not created from thin air
- Response includes `agent_number`, `required_stake`, `stake_locked`, and `next_stake`
Verified: Agent 2 rejected with 1K stake, rejected without ledger balance.

### 3.4 ✅ RESOLVED — Transfer Now Requires Ed25519 Signature Authentication
**Fix (2026-05-18):** `handleTransfer` now requires:
- `pubkey` — sender's Ed25519 public key (hex-encoded, 64 chars)
- `signature` — Ed25519 signature over canonical message `transfer:{from}:{to}:{amount}:{nonce}:{timestamp}`
- `nonce` — anti-replay nonce
- `timestamp` — must be within ±5 min of server time
- Address derivation verified: `PubkeyToAddress(pubkey)` must match `from` address
- New `crypto/signatures.go` module: key generation, signing, verification
- New `VerifyTransfer()` validates the entire chain: pubkey → address → signature → message
Verified: Rejects unsigned, invalid signature, wrong signer, expired timestamp.

### 3.5 ✅ PASS — CORS Headers Are Set
The `corsMiddleware` correctly sets `Access-Control-Allow-Origin: *`. Acceptable for testnet; should be restricted for mainnet.

### 3.6 ✅ PASS — Rate Limiter Is Applied
`rl.Middleware(s.mux)` wraps all routes. Token-bucket rate limiting is active.

### 3.7 ⚠️ LOW — Error Messages May Leak Internal State
Some error messages expose internal details (e.g., "mempool full (10000 transactions)", balance amounts, addresses). Not critical for testnet but should be sanitized for production.

### 3.8 ⚠️ LOW — `handleIntelligenceQuery` Oracle Pricing Logic Is Fragile
```go
if agentID != "" {
    _, regErr := s.registry.GetAgent(agentID)
    if regErr != nil {
        // Charge 10 $ALPHA
        if err := s.ledger.BurnSupply(agentAddr, oracleExternalBurn); err != nil {
            // Error means they can't pay → denied
```
**Issue:** If an unregistered agent queries with an address that has no balance, they get a payment error. But they could also query without an `agent_id` param, bypassing the charge entirely (the check `if agentID != ""` is skipped).

**Fix:** Always require `agent_id` or `from_address` for queries. Make payment mandatory.

### 3.9 ℹ️ INFO — P2P Block Handler Has No Recursion Guard
`handleP2PBlock` → `p2pNode.HandleIncomingBlock` → `blockHandler` → gossip → `handleP2PBlock`. The gossip function excludes the sender address to prevent direct loops, but a triangle of 3 nodes could create infinite gossip loops.

**Fix:** Add a "seen blocks" cache (e.g., LRU of last 1000 block hashes) and skip already-seen blocks.

---

## Summary

| Severity | Count | Key Issues | Status |
|----------|-------|------------|--------|
| 🔴 HIGH | 1 | No request body size limits (DoS) | ✅ RESOLVED |
| 🟡 MEDIUM | 4 | ZK verification no-op, transfer no auth, registration no sybil protection, proof depletion on quorum failure | 3/4 RESOLVED |
| 🟢 LOW | 4 | Burn inconsistency, error message leaking, oracle pricing bypass, gossip loop potential | Open |
| ℹ️ INFO | 4 | Various hardening suggestions | Open |

**Resolved in v0.3.1 (2026-05-18):**
1. ✅ Transfer authentication — Ed25519 signatures required (crypto/signatures.go + handleTransfer)
2. ✅ Body size limits — MaxBytesReader (1MB) on all 13 JSON handlers
3. ✅ ZK commitment verification — `verifyCommitment` now compares expected == CommitmentHash
4. ✅ Sybil protection — Exponential stake enforcement + ledger balance verification

**Remaining Medium:**
- `RunConsensus` proof depletion on quorum failure (consensus/poi.go ~line 196)

**Before Mainnet:**
5. Fix burn semantics consistency (choose one model)
6. Add P2P block deduplication cache
7. Restrict CORS in production
8. Add nonce-based replay protection to transfers
