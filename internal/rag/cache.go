// Package rag (cache.go): an in-memory answer cache keyed by (normalized
// question, KB epoch), so a repeated question against an unchanged KB does not
// re-spend a real subscription-agent turn. Get/Put is an interface specifically
// so rag.Ask's caching path is unit-testable with a fake, with no dependency on
// LRUCache's own eviction semantics.
package rag

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// AnswerCache is a Get/Put cache for synthesized answers, keyed by CacheKey's
// output. Production wires an *LRUCache; tests inject a fake.
type AnswerCache interface {
	Get(key string) (Answer, bool)
	Put(key string, a Answer)
}

// CacheKey derives a deterministic key from a question and the KB epoch it was
// answered against. Normalizing the question (lowercased, whitespace-collapsed)
// means trivial variations ("What is Pix?" vs "  what   is pix?  ") share an
// entry. Folding the epoch into the key means a KB update (a new epoch from
// `pixkb ingest` or an agent write-back) invalidates every prior answer purely
// by producing a different key — no explicit eviction pass is needed, and a
// stale answer from an old epoch can never be returned under the new epoch's
// key.
func CacheKey(question string, epoch int) string {
	norm := strings.ToLower(strings.Join(strings.Fields(question), " "))
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", epoch, norm)))
	return hex.EncodeToString(sum[:])
}

// LRUCache is a fixed-capacity, in-memory, thread-safe, least-recently-used
// AnswerCache. Built on container/list (stdlib) — no new go.mod dependency.
// It holds process-lifetime only: for the MCP server (a long-running process
// serving many kb_ask calls) this is where the benefit actually accrues; for
// the CLI (one process per invocation) it is wired for interface symmetry and
// the --no-cache debugging escape hatch, but a single `pixkb ask` run cannot
// itself produce a hit.
type LRUCache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	items    map[string]*list.Element
}

type lruEntry struct {
	key string
	ans Answer
}

// NewLRUCache builds an LRUCache holding up to capacity entries. capacity <= 0
// is normalized to 128 rather than producing a cache that evicts on every Put.
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 128
	}
	return &LRUCache{capacity: capacity, ll: list.New(), items: make(map[string]*list.Element)}
}

// Get returns the cached Answer for key, if present, and promotes it to
// most-recently-used.
func (c *LRUCache) Get(key string) (Answer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return Answer{}, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*lruEntry).ans, true
}

// Put inserts or updates key's Answer, evicting the least-recently-used entry
// once the cache is over capacity.
func (c *LRUCache) Put(key string, a Answer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry).ans = a
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&lruEntry{key: key, ans: a})
	c.items[key] = el
	if c.ll.Len() > c.capacity {
		if oldest := c.ll.Back(); oldest != nil {
			c.ll.Remove(oldest)
			delete(c.items, oldest.Value.(*lruEntry).key)
		}
	}
}
