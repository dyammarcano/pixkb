package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"pixkb/internal/embed"
)

// OpenAIEmbedder implements embed.Embedder against the OpenAI embeddings API
// (text-embedding-3-*): the same model embeds both concepts (at ingest) and
// queries (at search), so the vector arm is not near-random hashing noise.
//
// OPT-IN DEV-ONLY. This is a METERED API and therefore VIOLATES the air-gap
// rule (subscription agents only, no metered API). It is NOT the default and
// NOT the air-gap recall path — pointing BaseURL at a local OpenAI-compatible
// server is the only way to use it offline. The air-gap-compliant path to
// stronger recall is the agy agent fleet curating over pixdb (BACKLOG P2).
// See docs/agents-usage-signals.md and the air-gap memory.
type OpenAIEmbedder struct {
	APIKey  string
	Model   string
	Dims    int
	BaseURL string
	HTTP    *http.Client
}

// NewOpenAIEmbedder builds an embedder. model "" defaults to
// text-embedding-3-small; dims <= 0 defaults to 1536. The key comes from
// OPENAI_API_KEY; BaseURL from OPENAI_BASE_URL (default api.openai.com) so a
// local compatible server can be substituted.
func NewOpenAIEmbedder(model string, dims int) (*OpenAIEmbedder, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("openai embedder: OPENAI_API_KEY not set")
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	if dims <= 0 {
		dims = 1536
	}
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return &OpenAIEmbedder{
		APIKey:  key,
		Model:   model,
		Dims:    dims,
		BaseURL: base,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (e *OpenAIEmbedder) Name() string { return "openai:" + e.Model }
func (e *OpenAIEmbedder) Dim() int     { return e.Dims }

type embedReq struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embedResp struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed returns one vector per input text, preserving input order.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedReq{Model: e.Model, Input: texts, Dimensions: e.Dims})
	if err != nil {
		return nil, fmt.Errorf("openai embed: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embed: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := e.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var er embedResp
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("openai embed: decode (status %d): %w", resp.StatusCode, err)
	}
	if er.Error != nil {
		return nil, fmt.Errorf("openai embed: api error (status %d): %s", resp.StatusCode, er.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embed: status %d", resp.StatusCode)
	}
	if len(er.Data) != len(texts) {
		return nil, fmt.Errorf("openai embed: got %d vectors for %d inputs", len(er.Data), len(texts))
	}
	// The API may return data out of order; sort by index to match inputs.
	sort.Slice(er.Data, func(i, j int) bool { return er.Data[i].Index < er.Data[j].Index })
	out := make([][]float32, len(er.Data))
	for i := range er.Data {
		out[i] = er.Data[i].Embedding
	}
	return out, nil
}

var _ embed.Embedder = (*OpenAIEmbedder)(nil)
