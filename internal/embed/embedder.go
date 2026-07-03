package embed

import "context"

// Embedder converts text strings into fixed-dimension float32 vectors.
type Embedder interface {
	// Embed converts a slice of text strings to their vector representations.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim returns the dimensionality of the embeddings.
	Dim() int
	// Name returns the name of the embedder (e.g., "hashing").
	Name() string
}
