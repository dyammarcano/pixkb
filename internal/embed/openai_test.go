package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		var body openaiEmbedReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		resp := openaiEmbedResp{}
		for i := len(body.Input) - 1; i >= 0; i-- { // out of order to test sorting
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{Index: i, Embedding: []float32{float32(i), 0.5}})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL)
	e, err := NewOpenAIEmbedder("", 2)
	if err != nil {
		t.Fatalf("new embedder: %v", err)
	}
	vecs, err := e.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("want 3 vecs, got %d", len(vecs))
	}
	for i := range vecs {
		if vecs[i][0] != float32(i) {
			t.Errorf("vec %d out of input order: %v", i, vecs[i])
		}
	}
}

func TestOpenAIEmbedderNoKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := NewOpenAIEmbedder("", 0); err == nil {
		t.Error("want error when OPENAI_API_KEY unset")
	}
}

// TestOpenAIEmbedderGappedIndex confirms a response with the right count but a
// duplicated/gapped index set is rejected rather than silently mis-pairing
// vectors with inputs.
func TestOpenAIEmbedderGappedIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body openaiEmbedReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		resp := openaiEmbedResp{}
		// Correct count (2) but indices {0, 0} — a duplicate, no index 1.
		for _, idx := range []int{0, 0} {
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{Index: idx, Embedding: []float32{float32(idx), 0.5}})
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL)
	e, err := NewOpenAIEmbedder("", 2)
	if err != nil {
		t.Fatalf("new embedder: %v", err)
	}
	if _, err := e.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("want error on duplicate/gapped index, got nil")
	}
}
