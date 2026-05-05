package ingestDocuments

import (
	"strings"
	"testing"

	"github.com/mpandav-tibco/flogo-extensions/vectordb"
	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb/connector"
	mockclient "github.com/mpandav-tibco/flogo-extensions/vectordb/testutil/mock"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/mapper"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeInitContext is a minimal activity.InitContext used to test New().
type fakeInitContext struct {
	settings map[string]interface{}
	conn     *vectordbconnector.VectorDBConnection
}

func (f *fakeInitContext) Settings() map[string]interface{} { return f.settings }
func (f *fakeInitContext) Logger() log.Logger               { return log.RootLogger() }
func (f *fakeInitContext) GetConnection(setting string) (connection.Manager, error) {
	return f.conn, nil
}
func (f *fakeInitContext) Name() string                  { return "test-activity" }
func (f *fakeInitContext) HostName() string              { return "test-flow" }
func (f *fakeInitContext) MapperFactory() mapper.Factory { return nil }
func (f *fakeInitContext) Host() activity.Host           { return nil }

// settingsToMap converts a Settings struct into the map[string]interface{} that
// New() expects. Connection is injected separately via fakeInitContext.GetConnection.
func settingsToMap(s *Settings) map[string]interface{} {
	m := map[string]interface{}{
		"embeddingProvider":   s.EmbeddingProvider,
		"embeddingAPIKey":     s.EmbeddingAPIKey,
		"embeddingBaseURL":    s.EmbeddingBaseURL,
		"embeddingModel":      s.EmbeddingModel,
		"embeddingDimensions": s.EmbeddingDimensions,
		"contentField":        s.ContentField,
		"defaultCollection":   s.DefaultCollection,
		"timeoutSeconds":      s.TimeoutSeconds,
		"embeddingBatchSize":  s.EmbeddingBatchSize,
		"enableChunking":      s.EnableChunking,
		"chunkStrategy":       s.ChunkStrategy,
		"chunkSize":           s.ChunkSize,
		"chunkOverlap":        s.ChunkOverlap,
	}
	return m
}

// Ensure fakeInitContext satisfies activity.InitContext at compile time.
var _ activity.InitContext = (*fakeInitContext)(nil)

// ── validateChunkConfig ───────────────────────────────────────────────────────

func TestValidateChunkConfig_ValidStrategies(t *testing.T) {
	strategies := []ChunkStrategy{
		ChunkStrategyFixed, ChunkStrategySentence,
		ChunkStrategyParagraph, ChunkStrategyHeading,
	}
	for _, s := range strategies {
		cfg := ChunkConfig{Strategy: s, Size: 500, Overlap: 50}
		assert.NoError(t, validateChunkConfig(cfg), "strategy=%s", s)
	}
}

func TestValidateChunkConfig_UnknownStrategy(t *testing.T) {
	err := validateChunkConfig(ChunkConfig{Strategy: "word", Size: 500})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown chunk strategy")
}

func TestValidateChunkConfig_FixedRequiresPositiveSize(t *testing.T) {
	err := validateChunkConfig(ChunkConfig{Strategy: ChunkStrategyFixed, Size: 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunkSize must be > 0")
}

func TestValidateChunkConfig_OverlapMustBeLessThanSize(t *testing.T) {
	err := validateChunkConfig(ChunkConfig{Strategy: ChunkStrategyFixed, Size: 100, Overlap: 100})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunkOverlap")
}

func TestValidateChunkConfig_NegativeOverlapRejected(t *testing.T) {
	err := validateChunkConfig(ChunkConfig{Strategy: ChunkStrategyFixed, Size: 500, Overlap: -1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunkOverlap must be >= 0")
}

// ── chunkFixed ────────────────────────────────────────────────────────────────

func TestChunkFixed_ShortTextReturnedAsIs(t *testing.T) {
	chunks := chunkFixed("hello world", 1000, 0)
	assert.Equal(t, []string{"hello world"}, chunks)
}

func TestChunkFixed_ExactWindowSize(t *testing.T) {
	text := strings.Repeat("a", 100)
	chunks := chunkFixed(text, 100, 0)
	assert.Equal(t, 1, len(chunks))
	assert.Equal(t, 100, len(chunks[0]))
}

func TestChunkFixed_SplitsIntoMultipleChunks(t *testing.T) {
	// 300 chars split with size=100, overlap=0 → 3 chunks
	text := strings.Repeat("x", 300)
	chunks := chunkFixed(text, 100, 0)
	assert.Equal(t, 3, len(chunks))
	for _, c := range chunks {
		assert.Equal(t, 100, len(c))
	}
}

func TestChunkFixed_OverlapProducesExtraChunks(t *testing.T) {
	// 200 chars, size=100, overlap=50 → step=50 → chunks at 0,50,100,150 → 4 chunks
	text := strings.Repeat("y", 200)
	chunks := chunkFixed(text, 100, 50)
	assert.Greater(t, len(chunks), 2, "overlap should produce more chunks than no-overlap")
}

func TestChunkFixed_LastChunkShorterThanSize(t *testing.T) {
	text := strings.Repeat("z", 250) // 250 chars, size=100, overlap=0 → 2 full + 1 partial
	chunks := chunkFixed(text, 100, 0)
	assert.Equal(t, 3, len(chunks))
	assert.Equal(t, 50, len(chunks[2]))
}

func TestChunkFixed_DefaultSizeWhenZero(t *testing.T) {
	// size=0 should default to 1000; text shorter than 1000 → single chunk
	text := strings.Repeat("a", 500)
	chunks := chunkFixed(text, 0, 0)
	assert.Equal(t, 1, len(chunks))
}

// ── chunkParagraph ────────────────────────────────────────────────────────────

func TestChunkParagraph_SplitsOnBlankLine(t *testing.T) {
	text := "first paragraph\n\nsecond paragraph\n\nthird paragraph"
	chunks := chunkParagraph(text)
	assert.Equal(t, 3, len(chunks))
	assert.Equal(t, "first paragraph", chunks[0])
	assert.Equal(t, "second paragraph", chunks[1])
	assert.Equal(t, "third paragraph", chunks[2])
}

func TestChunkParagraph_NoBlanksReturnsSingleChunk(t *testing.T) {
	text := "no blank lines here\njust newlines"
	chunks := chunkParagraph(text)
	assert.Equal(t, 1, len(chunks))
}

func TestChunkParagraph_WindowsLineEndingsNormalised(t *testing.T) {
	text := "para one\r\n\r\npara two"
	chunks := chunkParagraph(text)
	assert.Equal(t, 2, len(chunks))
	assert.Equal(t, "para one", chunks[0])
	assert.Equal(t, "para two", chunks[1])
}

func TestChunkParagraph_EmptyChunksSkipped(t *testing.T) {
	text := "para one\n\n\n\npara two" // multiple blank lines
	chunks := chunkParagraph(text)
	assert.Equal(t, 2, len(chunks))
}

// ── chunkSentence ─────────────────────────────────────────────────────────────

func TestChunkSentence_SingleChunkWhenTextShort(t *testing.T) {
	text := "This is a short sentence."
	chunks := chunkSentence(text, 1000)
	assert.Equal(t, 1, len(chunks))
}

func TestChunkSentence_SplitsWhenSentencesExceedSize(t *testing.T) {
	// Each sentence is ~30 chars; size=50 → 2 sentences per chunk
	text := "First sentence here. Second sentence here. Third sentence here. Fourth sentence here."
	chunks := chunkSentence(text, 50)
	assert.Greater(t, len(chunks), 1, "should split into multiple chunks")
}

func TestChunkSentence_ExclamationAndQuestionSplit(t *testing.T) {
	text := "Is this working? Yes it is! Great news."
	chunks := chunkSentence(text, 20)
	// At size=20, each sentence should be its own chunk
	assert.Greater(t, len(chunks), 1)
}

func TestChunkSentence_DefaultSizeWhenZero(t *testing.T) {
	text := "Short sentence. Another one."
	chunks := chunkSentence(text, 0)
	assert.Equal(t, 1, len(chunks), "both sentences fit within default 1000 chars")
}

// ── chunkHeading ──────────────────────────────────────────────────────────────

func TestChunkHeading_SplitsOnH1(t *testing.T) {
	text := "# Introduction\nsome intro text\n# Details\ndetail text"
	chunks := chunkHeading(text)
	assert.Equal(t, 2, len(chunks))
	assert.True(t, strings.HasPrefix(chunks[0], "# Introduction"))
	assert.True(t, strings.HasPrefix(chunks[1], "# Details"))
}

func TestChunkHeading_SplitsOnH2AndH3(t *testing.T) {
	text := "## Overview\noverview text\n### Sub-section\nsub text\n## Next\nnext text"
	chunks := chunkHeading(text)
	assert.Equal(t, 3, len(chunks))
}

func TestChunkHeading_PreambleBeforeFirstHeading(t *testing.T) {
	text := "Preamble content here.\n\n## First Section\nsection content"
	chunks := chunkHeading(text)
	assert.Equal(t, 2, len(chunks))
	assert.Equal(t, "Preamble content here.", chunks[0])
}

func TestChunkHeading_NoHeadingsReturnsSingleChunk(t *testing.T) {
	text := "just plain text\nno headings at all"
	chunks := chunkHeading(text)
	assert.Equal(t, 1, len(chunks))
}

func TestChunkHeading_WindowsLineEndingsNormalised(t *testing.T) {
	text := "## Heading One\r\nContent one\r\n## Heading Two\r\nContent two"
	chunks := chunkHeading(text)
	assert.Equal(t, 2, len(chunks))
}

func TestChunkHeading_HeadingTitlePreservedInChunk(t *testing.T) {
	text := "## JDBC Connection Pools\nJDBC timeout occurs when pool is exhausted."
	chunks := chunkHeading(text)
	assert.Equal(t, 1, len(chunks))
	assert.Contains(t, chunks[0], "## JDBC Connection Pools")
	assert.Contains(t, chunks[0], "JDBC timeout occurs")
}

// ── expandChunks ──────────────────────────────────────────────────────────────

func TestExpandChunks_ChunkIDsFormatted(t *testing.T) {
	docs := []RawDocument{
		{ID: "doc1", Text: "para one\n\npara two", Metadata: map[string]interface{}{}},
	}
	cfg := ChunkConfig{Strategy: ChunkStrategyParagraph}
	result := expandChunks(docs, cfg)
	require.Equal(t, 2, len(result))
	assert.Equal(t, "doc1-chunk-0", result[0].ID)
	assert.Equal(t, "doc1-chunk-1", result[1].ID)
}

func TestExpandChunks_ChunkIDEmptyWhenSourceIDEmpty(t *testing.T) {
	// When parent has no ID, chunk ID is empty → UUID will be assigned downstream.
	docs := []RawDocument{
		{ID: "", Text: "para one\n\npara two", Metadata: map[string]interface{}{}},
	}
	cfg := ChunkConfig{Strategy: ChunkStrategyParagraph}
	result := expandChunks(docs, cfg)
	require.Equal(t, 2, len(result))
	assert.Equal(t, "", result[0].ID, "empty parent ID → empty chunk ID for UUID assignment")
}

func TestExpandChunks_ProvenanceMetadataAttached(t *testing.T) {
	docs := []RawDocument{
		{ID: "page-42", Text: "## Section A\nContent A.\n## Section B\nContent B.", Metadata: map[string]interface{}{"team": "IPS"}},
	}
	cfg := ChunkConfig{Strategy: ChunkStrategyHeading}
	result := expandChunks(docs, cfg)
	require.Equal(t, 2, len(result))

	for i, chunk := range result {
		assert.Equal(t, "page-42", chunk.Metadata["_source_id"])
		assert.Equal(t, i, chunk.Metadata["_chunk_index"])
		assert.Equal(t, 2, chunk.Metadata["_chunk_total"])
		assert.Equal(t, "heading", chunk.Metadata["_chunk_strategy"])
		assert.Equal(t, "IPS", chunk.Metadata["team"], "parent metadata must be inherited")
	}
}

func TestExpandChunks_MetadataIsolation(t *testing.T) {
	// Modifying one chunk's metadata must not affect another chunk.
	docs := []RawDocument{
		{ID: "d1", Text: "para one\n\npara two", Metadata: map[string]interface{}{}},
	}
	cfg := ChunkConfig{Strategy: ChunkStrategyParagraph}
	result := expandChunks(docs, cfg)
	require.Equal(t, 2, len(result))
	result[0].Metadata["injected"] = "yes"
	assert.Nil(t, result[1].Metadata["injected"], "chunks must have independent metadata maps")
}

func TestExpandChunks_MultipleSourceDocuments(t *testing.T) {
	docs := []RawDocument{
		{ID: "d1", Text: "para A\n\npara B", Metadata: map[string]interface{}{}},
		{ID: "d2", Text: "para C\n\npara D\n\npara E", Metadata: map[string]interface{}{}},
	}
	cfg := ChunkConfig{Strategy: ChunkStrategyParagraph}
	result := expandChunks(docs, cfg)
	assert.Equal(t, 5, len(result), "d1→2 chunks + d2→3 chunks = 5 total")
	assert.Equal(t, "d1-chunk-0", result[0].ID)
	assert.Equal(t, "d1-chunk-1", result[1].ID)
	assert.Equal(t, "d2-chunk-0", result[2].ID)
}

// ── Eval integration: chunking enabled ───────────────────────────────────────

func TestIngestDocuments_ChunkingParagraph(t *testing.T) {
	srv := batchEmbedServer(4)
	defer srv.Close()

	mc := &mockclient.VectorDBClient{}
	// 1 input doc with 3 paragraphs → 3 upsert calls in one batch
	mc.On("UpsertDocuments", mock.Anything, "kb", mock.MatchedBy(func(docs interface{}) bool {
		d, ok := docs.([]vectordb.Document)
		return ok && len(d) == 3
	})).Return(nil)

	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingAPIKey:   "test-key",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
			EnableChunking:    true,
			ChunkStrategy:     "paragraph",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "kb",
		"documents": []interface{}{
			map[string]interface{}{
				"id":   "page1",
				"text": "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.",
			},
		},
	}}

	done, err := act.Eval(ctx)
	require.NoError(t, err)
	assert.True(t, done)

	out := getOutput(ctx.outputs)
	assert.True(t, out.Success)
	assert.Equal(t, 3, out.IngestedCount, "ingestedCount = chunk count")
	assert.Equal(t, 3, out.ChunksCreated)
	assert.Equal(t, 1, out.SourceDocumentCount, "sourceDocumentCount = input docs before chunking")
	mc.AssertExpectations(t)
}

func TestIngestDocuments_ChunkingHeading(t *testing.T) {
	srv := batchEmbedServer(4)
	defer srv.Close()

	mc := &mockclient.VectorDBClient{}
	// 1 doc with 2 headings → 2 chunks
	mc.On("UpsertDocuments", mock.Anything, "runbooks", mock.MatchedBy(func(docs interface{}) bool {
		d, ok := docs.([]vectordb.Document)
		return ok && len(d) == 2
	})).Return(nil)

	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
			EnableChunking:    true,
			ChunkStrategy:     "heading",
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "runbooks",
		"documents": []interface{}{
			map[string]interface{}{
				"id":   "confluence-123",
				"text": "## Overview\nApplication overview text.\n## Troubleshooting\nJDBC timeout steps.",
				"metadata": map[string]interface{}{
					"url":  "https://confluence.example.com/pages/123",
					"team": "IPS",
				},
			},
		},
	}}

	done, err := act.Eval(ctx)
	require.NoError(t, err)
	assert.True(t, done)

	out := getOutput(ctx.outputs)
	assert.True(t, out.Success)
	assert.Equal(t, 2, out.ChunksCreated)
	assert.Equal(t, 1, out.SourceDocumentCount)
	mc.AssertExpectations(t)
}

func TestIngestDocuments_ChunkingDisabled_CountsMatch(t *testing.T) {
	srv := batchEmbedServer(4)
	defer srv.Close()

	mc := &mockclient.VectorDBClient{}
	mc.On("UpsertDocuments", mock.Anything, "col", mock.Anything).Return(nil)

	act := &Activity{
		conn: newTestConn(mc),
		settings: &Settings{
			EmbeddingProvider: "OpenAI",
			EmbeddingBaseURL:  srv.URL + "/v1",
			EmbeddingModel:    "text-embedding-3-small",
			ContentField:      "text",
			EnableChunking:    false, // explicit off
		},
	}
	ctx := &fakeActivityContext{inputs: map[string]interface{}{
		"collectionName": "col",
		"documents": []interface{}{
			map[string]interface{}{"id": "a", "text": "doc a"},
			map[string]interface{}{"id": "b", "text": "doc b"},
		},
	}}

	done, err := act.Eval(ctx)
	require.NoError(t, err)
	assert.True(t, done)

	out := getOutput(ctx.outputs)
	assert.True(t, out.Success)
	assert.Equal(t, 2, out.IngestedCount)
	assert.Equal(t, 2, out.SourceDocumentCount, "no chunking: sourceDocumentCount == ingestedCount")
	assert.Equal(t, 2, out.ChunksCreated, "no chunking: chunksCreated == ingestedCount")
	mc.AssertExpectations(t)
}

func TestIngestDocuments_InvalidChunkStrategyRejectedAtInit(t *testing.T) {
	// validateChunkConfig is called inside New(); test it directly since
	// providing a full connection in a fake init context adds no extra value.
	err := validateChunkConfig(ChunkConfig{Strategy: "word", Size: 500})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown chunk strategy")
}

func TestIngestDocuments_FixedChunkOverlapTooLargeRejected(t *testing.T) {
	err := validateChunkConfig(ChunkConfig{Strategy: ChunkStrategyFixed, Size: 100, Overlap: 200})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chunkOverlap")
}
