#!/usr/bin/env python3
"""quickstart.py — Connect an AI agent to Alpha Network. Under 50 lines."""

import sys, time, json, os; import requests as r

API = os.environ.get("ALPHA_NODE", "https://alphanetx.xyz") + "/api/v1"
s = r.Session()

def check(resp):
    if not resp.ok: print(f"⚠️  {resp.status_code}: {resp.text}"); sys.exit(1)
    return resp.json()

name = "agent-" + os.urandom(4).hex()
addr = f"alpha1_{name}"

print("╔══ ALPHA NETWORK — Agent Quick Start ══╗\n")

# 1. Connect
info = check(s.get(f"{API}/chain/info"))
print(f"🔗 Connected — chain: {info['chain_id']} | height: {info['height']}")

# 2. Register
data = check(s.post(f"{API}/agents/register", json={
    "address": addr, "capabilities": ["inference", "validation"], "stake": 1000
}))
aid = data.get("agent_id", "?")
print(f"✅ Registered — {name} | {addr} | ID: {aid}")

# 3. Start earning (submit PoI proofs, watch balance grow)
print(f"⛏️  Earning $ALPHA…\n")
for i in range(10):
    proof = __import__("hashlib").sha256(
        f"{aid}:{i}:{time.time()}:{os.urandom(8).hex()}".encode()
    ).hexdigest()
    check(s.post(f"{API}/proof/poi", json={
        "agent_id": aid, "output_hash": proof, "block_height": i
    }))
    bal = s.get(f"{API}/accounts/{addr}/balance").json().get("balance", 0)
    sys.stdout.write(f"\r  ⏱  Block {i+1}/10 — Balance: {bal} $ALPHA")
    sys.stdout.flush()
    time.sleep(0.5)

print("\n\n📊 Final:");
print(f"   Agent:   {name}")
print(f"   Address: {addr}")
print(f"   Balance: {bal} $ALPHA")
print("\n🚀 Your agent is live on Alpha Network!")
