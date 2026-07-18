package rag

import (
	"context"
	"strings"
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
	cache.Put(cacheKeyFor("q", Options{Epoch: 7}), Answer{Text: "cached", Citations: []string{"a.md"}})

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
	cached, ok := cache.Get(cacheKeyFor("q", Options{Epoch: 3}))
	if !ok || cached.Text != ans.Text {
		t.Fatalf("cache must be populated after a miss, got %+v ok=%v", cached, ok)
	}
}

func TestAsk_NoPIIFilterNeverCachedNorLeaked(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	// The generated answer carries a CPF that RedactPII scrubs to [REDACTED:CPF].
	gen := &fakeGen{reply: `{"answer":"CPF 123.456.789-01","citations":["a.md"],"refused":false}`}
	cache := newFakeCache()

	// First call disables the PII filter (debug path): returns raw text and must
	// NOT populate the cache.
	raw, _, err := Ask(context.Background(), r, cs, gen, "q", Options{Cache: cache, Epoch: 5, NoPIIFilter: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw.Text, "123.456.789-01") {
		t.Fatalf("NoPIIFilter must return raw text, got %q", raw.Text)
	}
	if len(cache.m) != 0 {
		t.Fatalf("a NoPIIFilter answer must never be cached, cache has %d entries", len(cache.m))
	}

	// A subsequent normal call for the same question+epoch must re-synthesize and
	// return REDACTED text — never the raw text from the first call.
	gen.called = false
	red, _, err := Ask(context.Background(), r, cs, gen, "q", Options{Cache: cache, Epoch: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !gen.called {
		t.Fatal("the normal call must not be served from a NoPIIFilter cache entry")
	}
	if strings.Contains(red.Text, "123.456.789-01") || !strings.Contains(red.Text, "[REDACTED:CPF]") {
		t.Fatalf("normal call must return redacted text, got %q", red.Text)
	}
}

func TestCacheKeyFor_DiffersByScopeAndPIIFlag(t *testing.T) {
	base := Options{Epoch: 1, TopK: 5}
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"topk", Options{Epoch: 1, TopK: 10}},
		{"maxchars", Options{Epoch: 1, TopK: 5, MaxChars: 4000}},
		{"nopii", Options{Epoch: 1, TopK: 5, NoPIIFilter: true}},
		{"diversify", Options{Epoch: 1, TopK: 5, Diversify: true}},
		{"minscore", Options{Epoch: 1, TopK: 5, MinScore: 0.3}},
	} {
		if cacheKeyFor("q", base) == cacheKeyFor("q", tc.opts) {
			t.Fatalf("%s must change the cache key", tc.name)
		}
	}
}

func TestAsk_DifferentEpochBypassesStaleCache(t *testing.T) {
	r := &fakeRetriever{hits: []Hit{{ID: "a.md", Score: 1}}}
	cs := fakeSource{"a.md": concept("a.md", "A", "body", "doc:a")}
	gen := &fakeGen{reply: `{"answer":"fresh","citations":["a.md"],"refused":false}`}
	cache := newFakeCache()
	cache.Put(cacheKeyFor("q", Options{Epoch: 1}), Answer{Text: "stale from epoch 1"})

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
