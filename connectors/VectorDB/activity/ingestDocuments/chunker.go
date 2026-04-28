package ingestDocuments

import (
	"fmt"
	"regexp"
	"strings"
)

// ChunkStrategy selects the text-splitting algorithm.
type ChunkStrategy string

const (
	// ChunkStrategyFixed splits text into fixed-size character windows with
	// configurable overlap between consecutive chunks (LangChain-style).
	// Best for: uniform content where heading structure is absent.
	ChunkStrategyFixed ChunkStrategy = "fixed"

	// ChunkStrategySentence accumulates complete sentences until the chunk
	// would exceed ChunkSize characters, then starts a new chunk.
	// Best for: prose, support articles, log descriptions.
	ChunkStrategySentence ChunkStrategy = "sentence"

	// ChunkStrategyParagraph splits on one or more consecutive blank lines (\n\n).
	// Best for: structured plain-text documents, README files.
	ChunkStrategyParagraph ChunkStrategy = "paragraph"

	// ChunkStrategyHeading splits on Markdown ATX headings (# through ######).
	// Each heading and its content body becomes one chunk. The heading title is
	// preserved as the first line of the chunk for context.
	// Best for: Confluence pages exported as Markdown, wiki articles.
	ChunkStrategyHeading ChunkStrategy = "heading"
)

// ChunkConfig holds the resolved chunking parameters derived from Settings.
type ChunkConfig struct {
	Strategy ChunkStrategy
	Size     int // target chunk size in characters; used by fixed and sentence
	Overlap  int // character overlap between adjacent chunks; fixed only
}

// headingRe matches any Markdown ATX heading line (# through ######).
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s`)

// sentenceEndRe matches sentence-ending punctuation followed by whitespace.
var sentenceEndRe = regexp.MustCompile(`[.!?]+[\s]+`)

// validateChunkConfig returns an error if the config is self-inconsistent.
func validateChunkConfig(cfg ChunkConfig) error {
	switch cfg.Strategy {
	case ChunkStrategyFixed, ChunkStrategySentence, ChunkStrategyParagraph, ChunkStrategyHeading:
		// valid
	default:
		return fmt.Errorf("unknown chunk strategy %q: must be one of fixed, sentence, paragraph, heading", cfg.Strategy)
	}
	if cfg.Strategy == ChunkStrategyFixed && cfg.Size <= 0 {
		return fmt.Errorf("chunkSize must be > 0 when strategy is 'fixed'")
	}
	if cfg.Overlap < 0 {
		return fmt.Errorf("chunkOverlap must be >= 0")
	}
	if cfg.Strategy == ChunkStrategyFixed && cfg.Overlap >= cfg.Size {
		return fmt.Errorf("chunkOverlap (%d) must be less than chunkSize (%d)", cfg.Overlap, cfg.Size)
	}
	return nil
}

// expandChunks takes the parsed input documents and, for each document, splits
// its Text field according to cfg. The returned slice replaces the input slice:
// each chunk becomes an independent RawDocument inheriting the parent's metadata
// plus provenance keys (_source_id, _chunk_index, _chunk_total, _chunk_strategy).
//
// When EnableChunking is false this function is never called; callers pass
// through rawDocs unchanged.
func expandChunks(docs []RawDocument, cfg ChunkConfig) []RawDocument {
	var result []RawDocument
	for _, doc := range docs {
		chunks := chunkText(doc.Text, cfg)
		total := len(chunks)
		for i, chunk := range chunks {
			// Build chunk ID: "<parent-id>-chunk-<i>" or leave blank for UUID assignment.
			chunkID := ""
			if doc.ID != "" {
				chunkID = fmt.Sprintf("%s-chunk-%d", doc.ID, i)
			}

			// Deep-copy parent metadata so each chunk has an independent map.
			meta := make(map[string]interface{}, len(doc.Metadata)+4)
			for k, v := range doc.Metadata {
				meta[k] = v
			}
			// Provenance fields — written under reserved _ prefix to avoid clashes.
			meta["_source_id"] = doc.ID
			meta["_chunk_index"] = i
			meta["_chunk_total"] = total
			meta["_chunk_strategy"] = string(cfg.Strategy)

			result = append(result, RawDocument{
				ID:       chunkID,
				Text:     chunk,
				Metadata: meta,
			})
		}
	}
	return result
}

// chunkText dispatches to the appropriate splitting implementation.
// Returns at least one element (the original text) even when no split occurs.
func chunkText(text string, cfg ChunkConfig) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}
	switch cfg.Strategy {
	case ChunkStrategyFixed:
		return chunkFixed(text, cfg.Size, cfg.Overlap)
	case ChunkStrategySentence:
		return chunkSentence(text, cfg.Size)
	case ChunkStrategyParagraph:
		return chunkParagraph(text)
	case ChunkStrategyHeading:
		return chunkHeading(text)
	default:
		return []string{text}
	}
}

// ── Strategy implementations ─────────────────────────────────────────────────

// chunkFixed splits text into windows of `size` runes, advancing by
// (size - overlap) runes each step. Overlap prevents context loss at
// boundaries (e.g. a sentence split across two chunks).
func chunkFixed(text string, size, overlap int) []string {
	if size <= 0 {
		size = 1000
	}
	if overlap < 0 {
		overlap = 0
	}
	// Safety: clamp overlap so step is always positive.
	if overlap >= size {
		overlap = size / 2
	}
	step := size - overlap
	runes := []rune(text)
	total := len(runes)
	if total <= size {
		return []string{text}
	}

	var chunks []string
	for start := 0; start < total; start += step {
		end := start + size
		if end > total {
			end = total
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == total {
			break
		}
	}
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// chunkSentence accumulates complete sentences until appending the next would
// exceed `size` characters, at which point a new chunk starts. Sentence
// boundaries are detected by [.!?] followed by whitespace.
func chunkSentence(text string, size int) []string {
	if size <= 0 {
		size = 1000
	}

	// Find sentence boundary positions while keeping the punctuation attached.
	locs := sentenceEndRe.FindAllStringIndex(text, -1)
	var sentences []string
	prev := 0
	for _, loc := range locs {
		end := loc[1]
		s := strings.TrimSpace(text[prev:end])
		if s != "" {
			sentences = append(sentences, s)
		}
		prev = end
	}
	// Trailing text after the last sentence boundary (e.g. no trailing period).
	if prev < len(text) {
		tail := strings.TrimSpace(text[prev:])
		if tail != "" {
			sentences = append(sentences, tail)
		}
	}
	if len(sentences) == 0 {
		return []string{text}
	}

	var chunks []string
	var buf strings.Builder
	for _, s := range sentences {
		// If adding this sentence would overflow the target size, flush first.
		if buf.Len() > 0 && buf.Len()+1+len(s) > size {
			chunk := strings.TrimSpace(buf.String())
			if chunk != "" {
				chunks = append(chunks, chunk)
			}
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(s)
	}
	if buf.Len() > 0 {
		chunk := strings.TrimSpace(buf.String())
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
	}
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// chunkParagraph splits on one or more consecutive blank lines. This maps
// naturally to Confluence pages exported as plain text or Markdown where
// logical sections are separated by blank lines.
func chunkParagraph(text string) []string {
	// Normalise Windows line endings before splitting.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.Split(text, "\n\n")
	var chunks []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			chunks = append(chunks, p)
		}
	}
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

// chunkHeading splits on Markdown ATX heading lines (# through ######). Each
// heading and all text until the next heading forms one chunk. The heading
// title is included as the first line of the chunk so retrieval preserves the
// section label — critical for Confluence-sourced runbooks and wikis.
//
// Text that appears before the first heading (e.g. page preamble) is returned
// as an implicit leading chunk if non-empty.
func chunkHeading(text string) []string {
	// Normalise Windows line endings.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")

	var chunks []string
	var buf strings.Builder

	flush := func() {
		chunk := strings.TrimSpace(buf.String())
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		buf.Reset()
	}

	for _, line := range lines {
		if headingRe.MatchString(line) {
			// Flush whatever was accumulated before this new heading.
			flush()
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(line)
	}
	flush()

	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}
