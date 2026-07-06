package ingestDocuments

import (
	"strings"
	"unicode"
)

// ChunkStrategy constants
const (
	ChunkFixed     = "fixed"
	ChunkSentence  = "sentence"
	ChunkParagraph = "paragraph"
	ChunkHeading   = "heading"
)

// expandChunks applies the chosen strategy to each text and returns all chunks.
func expandChunks(texts []string, strategy string, size, overlap int) []string {
	if size <= 0 {
		size = 512
	}
	if overlap < 0 {
		overlap = 0
	}
	if strategy == "" {
		strategy = ChunkFixed
	}

	var all []string
	for _, t := range texts {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		switch strategy {
		case ChunkSentence:
			all = append(all, chunkSentence(t, size, overlap)...)
		case ChunkParagraph:
			all = append(all, chunkParagraph(t, size, overlap)...)
		case ChunkHeading:
			all = append(all, chunkHeading(t, size, overlap)...)
		default:
			all = append(all, chunkFixed(t, size, overlap)...)
		}
	}
	return all
}

// chunkFixed splits text into fixed-size windows (by rune count) with overlap.
func chunkFixed(text string, size, overlap int) []string {
	runes := []rune(text)
	if len(runes) <= size {
		return []string{text}
	}
	step := size - overlap
	if step <= 0 {
		step = 1
	}
	var chunks []string
	for i := 0; i < len(runes); i += step {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}

// chunkSentence splits by sentence boundaries then re-aggregates into windows.
func chunkSentence(text string, size, overlap int) []string {
	sentences := splitSentences(text)
	return aggregateChunks(sentences, size, overlap)
}

// chunkParagraph splits by blank lines then re-aggregates.
func chunkParagraph(text string, size, overlap int) []string {
	parts := strings.Split(text, "\n\n")
	var paras []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			paras = append(paras, p)
		}
	}
	return aggregateChunks(paras, size, overlap)
}

// chunkHeading splits at Markdown headings (# lines).
func chunkHeading(text string, size, overlap int) []string {
	lines := strings.Split(text, "\n")
	var sections []string
	var cur strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") && cur.Len() > 0 {
			sections = append(sections, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		cur.WriteString(line)
		cur.WriteString("\n")
	}
	if cur.Len() > 0 {
		sections = append(sections, strings.TrimSpace(cur.String()))
	}
	return aggregateChunks(sections, size, overlap)
}

// aggregateChunks joins small pieces into windows of at most `size` runes.
func aggregateChunks(pieces []string, size, overlap int) []string {
	if len(pieces) == 0 {
		return nil
	}
	var chunks []string
	var buf strings.Builder
	for _, p := range pieces {
		candidate := buf.String()
		if candidate != "" {
			candidate += " " + p
		} else {
			candidate = p
		}
		if len([]rune(candidate)) > size && buf.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(buf.String()))
			// overlap: keep last `overlap` runes from the flushed chunk
			if overlap > 0 {
				prev := []rune(buf.String())
				start := len(prev) - overlap
				if start < 0 {
					start = 0
				}
				buf.Reset()
				buf.WriteString(string(prev[start:]))
				buf.WriteString(" ")
				buf.WriteString(p)
			} else {
				buf.Reset()
				buf.WriteString(p)
			}
		} else {
			buf.Reset()
			buf.WriteString(candidate)
		}
	}
	if buf.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(buf.String()))
	}
	return chunks
}

// splitSentences is a simple heuristic sentence splitter.
func splitSentences(text string) []string {
	var sentences []string
	var cur strings.Builder
	runes := []rune(text)
	for i, r := range runes {
		cur.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			// Check next char is space/upper
			if i+1 < len(runes) {
				next := runes[i+1]
				if next == ' ' || next == '\n' || unicode.IsUpper(next) {
					s := strings.TrimSpace(cur.String())
					if s != "" {
						sentences = append(sentences, s)
					}
					cur.Reset()
				}
			}
		}
	}
	if cur.Len() > 0 {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}
