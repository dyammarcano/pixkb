package embed

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
)

const defaultHashingDim = 256

type hashingEmbedder struct {
	dim int
}

// NewHashing returns a deterministic, pure-Go embedder that hashes
// lowercase character 3-grams into dim buckets and L2-normalizes the
// resulting vector. A non-positive dim selects the default of 256.
func NewHashing(dim int) Embedder {
	if dim <= 0 {
		dim = defaultHashingDim
	}
	return &hashingEmbedder{dim: dim}
}

func (h *hashingEmbedder) Name() string { return "hashing" }

func (h *hashingEmbedder) Dim() int { return h.dim }

func (h *hashingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = h.vectorize(t)
	}
	return out, nil
}

func (h *hashingEmbedder) vectorize(text string) []float32 {
	vec := make([]float32, h.dim)
	runes := []rune(strings.ToLower(text))
	const n = 3
	if len(runes) >= n {
		for i := 0; i+n <= len(runes); i++ {
			gram := string(runes[i : i+n])
			fh := fnv.New32a()
			_, _ = fh.Write([]byte(gram))
			bucket := int(fh.Sum32() % uint32(h.dim))
			vec[bucket]++
		}
	}
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return vec
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}
