// Package minilm is a no-CGo stub for github.com/amikos-tech/pure-onnx/embeddings/minilm.
// All functions return errNotSupported; no ONNX runtime is linked.
package minilm

import "errors"

var errNotSupported = errors.New("ONNX runtime not supported (pure-onnx stub)")

// Option configures an Embedder (stub — no-op).
type Option func(*config) error

type config struct{}

// WithMeanPooling selects mean pooling (no-op stub).
func WithMeanPooling() Option { return func(*config) error { return nil } }

// WithCLSPooling selects CLS pooling (no-op stub).
func WithCLSPooling() Option { return func(*config) error { return nil } }

// WithNoPooling disables pooling (no-op stub).
func WithNoPooling() Option { return func(*config) error { return nil } }

// WithL2Normalization enables L2 normalisation (no-op stub).
func WithL2Normalization() Option { return func(*config) error { return nil } }

// WithoutL2Normalization disables L2 normalisation (no-op stub).
func WithoutL2Normalization() Option { return func(*config) error { return nil } }

// WithSequenceLength sets the maximum sequence length (no-op stub).
func WithSequenceLength(_ int) Option { return func(*config) error { return nil } }

// WithEmbeddingDimension sets the embedding dimension (no-op stub).
func WithEmbeddingDimension(_ int64) Option { return func(*config) error { return nil } }

// WithMaxCachedBatchSessions sets the session cache limit (no-op stub).
func WithMaxCachedBatchSessions(_ int) Option { return func(*config) error { return nil } }

// WithTokenizerLibraryPath sets the tokenizer library path (no-op stub).
func WithTokenizerLibraryPath(_ string) Option { return func(*config) error { return nil } }

// WithoutTokenTypeIDsInput removes token-type-IDs input (no-op stub).
func WithoutTokenTypeIDsInput() Option { return func(*config) error { return nil } }

// WithInputOutputNames sets custom I/O tensor names (no-op stub).
func WithInputOutputNames(_, _, _, _ string) Option { return func(*config) error { return nil } }

// Embedder is a no-op stub for minilm.Embedder.
// It implements the defaultEFEmbedder interface required by
// github.com/amikos-tech/chroma-go/pkg/embeddings/default_ef.
type Embedder struct{}

// NewEmbedder always returns errNotSupported.
func NewEmbedder(_, _ string, _ ...Option) (*Embedder, error) {
	return nil, errNotSupported
}

// EmbedDocuments always returns errNotSupported.
func (e *Embedder) EmbedDocuments(_ []string) ([][]float32, error) {
	return nil, errNotSupported
}

// EmbedQuery always returns errNotSupported.
func (e *Embedder) EmbedQuery(_ string) ([]float32, error) {
	return nil, errNotSupported
}

// Close always returns errNotSupported.
func (e *Embedder) Close() error { return errNotSupported }
