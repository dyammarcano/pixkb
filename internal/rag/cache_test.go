package rag

import (
	"context"
	"testing"
)

type fakeCache struct{ m map[string]Answer }

func newFakeCache() *fakeCache                   { return &fakeCache{m: map[string]Answer{}} }
func (c *fakeCache) Get(k string) (Answer, bool) { a, ok := c.m[k]; return a, ok }
func (c *fakeCache) Put(k string, a Answer)      { c.m[k] = a }

func TestCacheKey_NormalizesWhitespaceAndCase(t *testing.T) {
	a := CacheKey("What is Pix?", 5)
	b := CacheKey("  what   is   pix?  ", 5)
	if a != b {
		t.Fatalf("normalized-equivalent questions must share a cache key: %q vs %q", a, b)
	}
}

func TestCacheKey_DiffersByEpoch(t *testing.T) {
	a := CacheKey("What is Pix?", 1)
	b := CacheKey("What is Pix?", 2)
	if a == b {
		t.Fatal("different epochs must produce different cache keys")
	}
}

func TestLRUCache_GetPutRoundTrip(t *testing.T) {
	c := NewLRUCache(2)
	if _, ok := c.Get("missing"); ok {
		t.Fatal("empty cache must miss")
	}
	c.Put("k1", Answer{Text: "v1"})
	got, ok := c.Get("k1")
	if !ok || got.Text != "v1" {
		t.Fatalf("expected v1, got %+v ok=%v", got, ok)
	}
}

func TestLRUCache_EvictsLeastRecentlyUsed(t *testing.T) {
	c := NewLRUCache(2)
	c.Put("k1", Answer{Text: "v1"})
	c.Put("k2", Answer{Text: "v2"})
	c.Put("k3", Answer{Text: "v3"}) // capacity 2: evicts k1
	if _, ok := c.Get("k1"); ok {
		t.Fatal("k1 should have been evicted")
	}
	if _, ok := c.Get("k2"); !ok {
		t.Fatal("k2 should still be present")
	}
	if _, ok := c.Get("k3"); !ok {
		t.Fatal("k3 should still be present")
	}
}

func TestLRUCache_GetPromotesToMostRecentlyUsed(t *testing.T) {
	c := NewLRUCache(2)
	c.Put("k1", Answer{Text: "v1"})
	c.Put("k2", Answer{Text: "v2"})
	c.Get("k1")                     // promote k1 ahead of k2
	c.Put("k3", Answer{Text: "v3"}) // should evict k2, not k1
	if _, ok := c.Get("k1"); !ok {
		t.Fatal("k1 was recently used, should not have been evicted")
	}
	if _, ok := c.Get("k2"); ok {
		t.Fatal("k2 should have been evicted")
	}
}

func TestAsk_CacheHitSkipsGenerator(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh","citations":["a.md"],"refused":false}`}
	cache := newFakeCache()
	cache.Put(CacheKey("q", 7), Answer{Text: "cached", Citations: []string{"a.md"}})

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{Cache: cache, Epoch: 7})
	if err != nil {
		t.Fatal(err)
	}
	if gen.called {
		t.Fatal("a cache hit must not spend an agent turn")
	}
	if ans.Text != "cached" {
		t.Fatalf("expected the cached answer, got %q", ans.Text)
	}
}

func TestAsk_CacheMissPopulatesCache(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh answer","citations":["a.md"],"refused":false}`}
	cache := newFakeCache()

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{Cache: cache, Epoch: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !gen.called {
		t.Fatal("a cache miss must still spend an agent turn")
	}
	cached, ok := cache.Get(CacheKey("q", 3))
	if !ok || cached.Text != ans.Text {
		t.Fatalf("cache must be populated after a miss, got %+v ok=%v", cached, ok)
	}
}

func TestAsk_DifferentEpochBypassesStaleCache(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh","citations":["a.md"],"refused":false}`}
	cache := newFakeCache()
	cache.Put(CacheKey("q", 1), Answer{Text: "stale from epoch 1"})

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{Cache: cache, Epoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !gen.called || ans.Text != "fresh" {
		t.Fatalf("a new epoch must not reuse a stale epoch's cache entry, got %+v called=%v", ans, gen.called)
	}
}

func TestAsk_NilCacheIsPreChangeBehavior(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh","citations":["a.md"],"refused":false}`}

	ans, _, err := Ask(context.Background(), r, cs, gen, "q", Options{}) // Cache left nil
	if err != nil {
		t.Fatal(err)
	}
	if !gen.called || ans.Text != "fresh" {
		t.Fatalf("Options{} (Cache==nil) must behave exactly like caching didn't exist, got %+v called=%v", ans, gen.called)
	}
}
