package ingestDocuments

import (
	"fmt"
	"regexp"
	"strings"
)

// ChunkStrategy selects the text-splitting algorithm.
type ChunkStrategy string

const (
	ChunkStrategyFixed     ChunkStrategy = "fixed"
	ChunkStrategySentence  ChunkStrategy = "sentence"
	ChunkStrategyParagraph ChunkStrategy = "paragraph"
	ChunkStrategyHeading   ChunkStrategy = "heading"
)

// ChunkConfig holds the resolved chunking parameters derived from Settings.
type ChunkConfig struct {
	Strategy ChunkStrategy
	Size     int
	Overlap  int
}

var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s`)
var sentenceEndRe = regexp.MustCompile(`[.!?]+[\s]+`)

func validateChunkConfig(cfg ChunkConfig) error {
	switch cfg.Strategy {
	case ChunkStrategyFixed, ChunkStrategySentence, ChunkStrategyParagraph, ChunkStrategyHeading:
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

const maxEmbeddingInputChars = 3500

func expandChunks(docs []RawDocument, cfg ChunkConfig) []RawDocument {
	var result []RawDocument
	for _, doc := range docs {
		chunks := chunkText(doc.Text, cfg)

		var capped []string
		for _, c := range chunks {
			if len([]rune(c)) > maxEmbeddingInputChars {
				capped = append(capped, chunkFixed(c, maxEmbeddingInputChars, 0)...)
			} else {
				capped = append(capped, c)
			}
		}
		chunks = capped
		total := len(chunks)
		for i, chunk := range chunks {
			chunkID := ""
			if doc.ID != "" {
				chunkID = fmt.Sprintf("%s-chunk-%d", doc.ID, i)
			}

			meta := make(map[string]interface{}, len(doc.Metadata)+4)
			for k, v := range doc.Metadata {
				meta[k] = v
			}
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

func chunkFixed(text string, size, overlap int) []string {
	if size <= 0 {
		size = 1000
	}
	if overlap < 0 {
		overlap = 0
	}
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

func chunkSentence(text string, size int) []string {
	if size <= 0 {
		size = 1000
	}

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

func chunkParagraph(text string) []string {
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

func chunkHeading(text string) []string {
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
