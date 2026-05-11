// Package store implements the persistent BadgerDB state store for Alpha Network.
// It provides both low-level key-value operations and higher-level typed
// methods for blocks, agents, balances, and intelligence records.
package store

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/alpha-network/alpha/chain/core"
	"github.com/alpha-network/alpha/chain/data"
)

// Key prefixes for logical namespacing within BadgerDB
const (
	prefixBlock      = "block:"
	prefixAgent      = "agent:"
	prefixBalance    = "balance:"
	prefixIntelRec   = "intel:"
	prefixMeta       = "meta:"
)

// ErrNotFound is returned when a key doesn't exist in the store.
var ErrNotFound = errors.New("not found")

// Store wraps a BadgerDB instance with typed Alpha Network operations.
type Store struct {
	db *badger.DB
}

// Open opens or creates a BadgerDB database at the specified directory.
// The directory is created if it doesn't exist.
func Open(dir string) (*Store, error) {
	opts := badger.DefaultOptions(dir)
	opts.Logger = nil // suppress BadgerDB's internal logging

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open badger db at %s: %w", dir, err)
	}
	return &Store{db: db}, nil
}

// Close flushes and closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// --- Low-level key-value methods ---

// Set stores a raw key-value pair.
func (s *Store) Set(key, value []byte) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})
}

// Get retrieves a value by key. Returns ErrNotFound if the key doesn't exist.
func (s *Store) Get(key []byte) ([]byte, error) {
	var val []byte
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return ErrNotFound
			}
			return err
		}
		val, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// Delete removes a key from the store. A missing key is not an error.
func (s *Store) Delete(key []byte) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

// Scan returns all values whose keys have the given prefix.
// Values are returned in lexicographic key order.
func (s *Store) Scan(prefix []byte) ([][]byte, error) {
	var results [][]byte
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			results = append(results, val)
		}
		return nil
	})
	return results, err
}

// --- Typed higher-level methods ---

// PutBlock persists a block to the store, keyed by height.
func (s *Store) PutBlock(block *core.Block) error {
	if block == nil {
		return errors.New("block cannot be nil")
	}
	val, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("marshal block: %w", err)
	}
	return s.Set(blockKey(block.Height), val)
}

// GetBlock retrieves a block by height. Returns ErrNotFound if not stored.
func (s *Store) GetBlock(height uint64) (*core.Block, error) {
	val, err := s.Get(blockKey(height))
	if err != nil {
		return nil, err
	}
	var block core.Block
	if err := json.Unmarshal(val, &block); err != nil {
		return nil, fmt.Errorf("unmarshal block: %w", err)
	}
	return &block, nil
}

// PutAgent persists an AgentIdentity to the store, keyed by AgentID.
func (s *Store) PutAgent(agent *core.AgentIdentity) error {
	if agent == nil {
		return errors.New("agent cannot be nil")
	}
	val, err := json.Marshal(agent)
	if err != nil {
		return fmt.Errorf("marshal agent: %w", err)
	}
	return s.Set(agentKey(agent.AgentID), val)
}

// GetAgent retrieves an agent by its AgentID. Returns ErrNotFound if not stored.
func (s *Store) GetAgent(id core.AgentID) (*core.AgentIdentity, error) {
	val, err := s.Get(agentKey(id))
	if err != nil {
		return nil, err
	}
	var agent core.AgentIdentity
	if err := json.Unmarshal(val, &agent); err != nil {
		return nil, fmt.Errorf("unmarshal agent: %w", err)
	}
	return &agent, nil
}

// ListAgents returns all stored agents.
func (s *Store) ListAgents() ([]*core.AgentIdentity, error) {
	vals, err := s.Scan([]byte(prefixAgent))
	if err != nil {
		return nil, err
	}
	agents := make([]*core.AgentIdentity, 0, len(vals))
	for _, v := range vals {
		var a core.AgentIdentity
		if err := json.Unmarshal(v, &a); err != nil {
			return nil, fmt.Errorf("unmarshal agent: %w", err)
		}
		agents = append(agents, &a)
	}
	return agents, nil
}

// PutBalance stores the $ALPHA balance for a given address.
func (s *Store) PutBalance(addr core.Address, amount core.Amount) error {
	val, err := json.Marshal(amount)
	if err != nil {
		return fmt.Errorf("marshal balance: %w", err)
	}
	return s.Set(balanceKey(addr), val)
}

// GetBalance retrieves the balance for an address. Returns 0 if not found.
func (s *Store) GetBalance(addr core.Address) (core.Amount, error) {
	val, err := s.Get(balanceKey(addr))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	var amount core.Amount
	if err := json.Unmarshal(val, &amount); err != nil {
		return 0, fmt.Errorf("unmarshal balance: %w", err)
	}
	return amount, nil
}

// PutIntelligenceRecord persists an intelligence record to the store.
// Key format: intel:<agentID>:<recordID>
func (s *Store) PutIntelligenceRecord(r *data.IntelligenceRecord) error {
	if r == nil {
		return errors.New("intelligence record cannot be nil")
	}
	val, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal intelligence record: %w", err)
	}
	return s.Set(intelKey(r.AgentID, r.RecordID), val)
}

// GetIntelligenceRecords returns all intelligence records for a given agent.
func (s *Store) GetIntelligenceRecords(agentID core.AgentID) ([]*data.IntelligenceRecord, error) {
	prefix := []byte(fmt.Sprintf("%s%s:", prefixIntelRec, agentID))
	vals, err := s.Scan(prefix)
	if err != nil {
		return nil, err
	}
	records := make([]*data.IntelligenceRecord, 0, len(vals))
	for _, v := range vals {
		var rec data.IntelligenceRecord
		if err := json.Unmarshal(v, &rec); err != nil {
			return nil, fmt.Errorf("unmarshal intelligence record: %w", err)
		}
		records = append(records, &rec)
	}
	return records, nil
}

// HasGenesisBlock returns true if the genesis block (height 0) is stored.
// Used during startup to determine if chain initialization is needed.
func (s *Store) HasGenesisBlock() bool {
	_, err := s.GetBlock(0)
	return err == nil
}

// PutMeta stores a metadata value (used for chain metadata like genesis hash, etc.)
func (s *Store) PutMeta(key string, value []byte) error {
	return s.Set([]byte(prefixMeta+key), value)
}

// GetMeta retrieves a metadata value. Returns ErrNotFound if absent.
func (s *Store) GetMeta(key string) ([]byte, error) {
	return s.Get([]byte(prefixMeta + key))
}

// --- Key helpers ---

func blockKey(height uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, height)
	return append([]byte(prefixBlock), buf...)
}

func agentKey(id core.AgentID) []byte {
	return []byte(prefixAgent + string(id))
}

func balanceKey(addr core.Address) []byte {
	return []byte(prefixBalance + string(addr))
}

func intelKey(agentID core.AgentID, recordID string) []byte {
	return []byte(fmt.Sprintf("%s%s:%s", prefixIntelRec, agentID, recordID))
}
