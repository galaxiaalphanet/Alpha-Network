// Package ledger implements the $ALPHA token account ledger
// Thread-safe, with overdraft protection, atomic transfers, and deflationary burn tracking
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alpha-network/alpha/chain/core"
)

// TxRecord is an immutable ledger entry for the transaction log
type TxRecord struct {
	TxID      string      `json:"tx_id"`
	Type      string      `json:"type"`
	From      core.Address `json:"from"`
	To        core.Address `json:"to"`
	Amount    core.Amount  `json:"amount"`
	Memo      string       `json:"memo"`
	Timestamp int64        `json:"timestamp"`
}

// Ledger manages all $ALPHA account balances and burn tracking
type Ledger struct {
	mu             sync.RWMutex
	balances       map[core.Address]core.Amount
	txLog          []*TxRecord
	totalBurned    core.Amount
	totalSupply    core.Amount
}

// NewLedger creates a Ledger with a fixed total supply
func NewLedger(totalSupply core.Amount) *Ledger {
	return &Ledger{
		balances:    make(map[core.Address]core.Amount),
		txLog:       make([]*TxRecord, 0, 1024),
		totalSupply: totalSupply,
	}
}

// Credit adds $ALPHA to an address (e.g., block rewards, genesis funding)
// Returns an error only if amount is non-positive.
func (l *Ledger) Credit(addr core.Address, amount core.Amount) error {
	if amount <= 0 {
		return errors.New("credit amount must be positive")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.balances[addr] += amount

	l.recordTx(&TxRecord{
		TxID:      l.genTxID("credit", string(addr)),
		Type:      "credit",
		To:        addr,
		Amount:    amount,
		Timestamp: time.Now().Unix(),
	})
	return nil
}

// Debit removes $ALPHA from an address with overdraft protection
func (l *Ledger) Debit(addr core.Address, amount core.Amount) error {
	if amount <= 0 {
		return errors.New("debit amount must be positive")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	bal := l.balances[addr]
	if bal < amount {
		return fmt.Errorf("insufficient balance: have %d, need %d", bal, amount)
	}
	l.balances[addr] -= amount

	l.recordTx(&TxRecord{
		TxID:      l.genTxID("debit", string(addr)),
		Type:      "debit",
		From:      addr,
		Amount:    amount,
		Timestamp: time.Now().Unix(),
	})
	return nil
}

// Transfer atomically moves $ALPHA from one address to another
// Returns the TxID or an error (overdraft / invalid amounts)
func (l *Ledger) Transfer(from, to core.Address, amount core.Amount, memo string) (string, error) {
	if amount <= 0 {
		return "", errors.New("transfer amount must be positive")
	}
	if from == to {
		return "", errors.New("from and to addresses must differ")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	bal := l.balances[from]
	if bal < amount {
		return "", fmt.Errorf("insufficient balance: have %d, need %d", bal, amount)
	}

	l.balances[from] -= amount
	l.balances[to] += amount

	txID := l.genTxID("transfer", string(from)+string(to))
	l.recordTx(&TxRecord{
		TxID:      txID,
		Type:      "transfer",
		From:      from,
		To:        to,
		Amount:    amount,
		Memo:      memo,
		Timestamp: time.Now().Unix(),
	})
	return txID, nil
}

// Balance returns the current balance for an address (0 if unknown)
func (l *Ledger) Balance(addr core.Address) core.Amount {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.balances[addr]
}

// BurnSupply permanently removes tokens from circulation (deflationary mechanism)
// Tokens are deducted from the given address and tracked as burned
func (l *Ledger) BurnSupply(addr core.Address, amount core.Amount) error {
	if amount <= 0 {
		return errors.New("burn amount must be positive")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	bal := l.balances[addr]
	if bal < amount {
		return fmt.Errorf("insufficient balance to burn: have %d, need %d", bal, amount)
	}

	l.balances[addr] -= amount
	l.totalBurned += amount

	l.recordTx(&TxRecord{
		TxID:      l.genTxID("burn", string(addr)),
		Type:      "burn",
		From:      addr,
		Amount:    amount,
		Memo:      "protocol burn",
		Timestamp: time.Now().Unix(),
	})
	return nil
}

// BurnFromProtocol burns tokens from the protocol treasury (no address needed)
// Used for fee burns that originate from the protocol itself
func (l *Ledger) BurnFromProtocol(amount core.Amount) error {
	if amount <= 0 {
		return errors.New("burn amount must be positive")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.totalBurned += amount
	l.totalSupply -= amount

	l.recordTx(&TxRecord{
		TxID:      l.genTxID("protocol_burn", "protocol"),
		Type:      "protocol_burn",
		Amount:    amount,
		Memo:      "protocol fee burn",
		Timestamp: time.Now().Unix(),
	})
	return nil
}

// CirculatingSupply returns total supply minus burned tokens
func (l *Ledger) CirculatingSupply() core.Amount {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.totalSupply - l.totalBurned
}

// TotalBurned returns the cumulative burned token count
func (l *Ledger) TotalBurned() core.Amount {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.totalBurned
}

// TxHistory returns the last n transaction records (all if n <= 0)
func (l *Ledger) TxHistory(n int) []*TxRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if n <= 0 || n >= len(l.txLog) {
		out := make([]*TxRecord, len(l.txLog))
		copy(out, l.txLog)
		return out
	}
	start := len(l.txLog) - n
	out := make([]*TxRecord, n)
	copy(out, l.txLog[start:])
	return out
}

// AddressHistory returns transactions involving a given address
func (l *Ledger) AddressHistory(addr core.Address, limit int) []*TxRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]*TxRecord, 0)
	for _, tx := range l.txLog {
		if tx.From == addr || tx.To == addr {
			result = append(result, tx)
		}
	}
	if limit > 0 && limit < len(result) {
		return result[len(result)-limit:]
	}
	return result
}

// Stats returns a summary of ledger state
func (l *Ledger) Stats() map[string]interface{} {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return map[string]interface{}{
		"total_supply":       l.totalSupply,
		"total_burned":       l.totalBurned,
		"circulating_supply": l.totalSupply - l.totalBurned,
		"account_count":      len(l.balances),
		"tx_count":           len(l.txLog),
	}
}

// --- internal helpers ---

func (l *Ledger) recordTx(tx *TxRecord) {
	l.txLog = append(l.txLog, tx)
}

func (l *Ledger) genTxID(txType, seed string) string {
	raw := fmt.Sprintf("%s:%s:%d", txType, seed, time.Now().UnixNano())
	h := sha256.Sum256([]byte(raw))
	return "tx_" + hex.EncodeToString(h[:])[:24]
}
