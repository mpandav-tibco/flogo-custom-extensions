package ingestDocuments

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb"
	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb/connector"
	vdbembed "github.com/mpandav-tibco/flogo-extensions/vectordb/embeddings"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() { _ = activity.Register(&Activity{}, New) }

// Activity combines embedding generation and VectorDB upsert into one step.
type Activity struct {
	settings *Settings
	conn     *vectordbconnector.VectorDBConnection
}

func (a *Activity) Metadata() *activity.Metadata { return activityMd }

func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	if err := metadata.MapToStruct(ctx.Settings(), s, true); err != nil {
		return nil, fmt.Errorf("vectordb-ingest: %w", err)
	}
	if s.Connection == nil {
		return nil, fmt.Errorf("vectordb-ingest: connection is required")
	}
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.VectorDBConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-ingest: invalid connection type, expected *VectorDBConnection")
	}

	// Resolve embedding credentials: inherit from connector when opted in.
	// Activity-level values (if set) always take precedence as an override.
	if s.UseConnectorEmbedding {
		connSettings := conn.GetSettings()
		if !connSettings.EnableEmbedding {
			ctx.Logger().Warnf("IngestDocuments: useConnectorEmbedding=true but connector does not have enableEmbedding set — falling back to activity-level settings")
		} else {
			if s.EmbeddingProvider == "" {
				s.EmbeddingProvider = connSettings.EmbeddingProvider
			}
			if s.EmbeddingAPIKey == "" {
				s.EmbeddingAPIKey = connSettings.EmbeddingAPIKey
			}
			if s.EmbeddingBaseURL == "" {
				s.EmbeddingBaseURL = connSettings.EmbeddingBaseURL
			}
		}
	}

	if s.EmbeddingModel == "" {
		s.EmbeddingModel = "text-embedding-3-small"
	}
	if s.ContentField == "" {
		s.ContentField = "text"
	}
	if s.TimeoutSeconds <= 0 {
		s.TimeoutSeconds = 60
	}

	// Resolve and validate chunking defaults at init time so
	// misconfiguration is caught before the first request arrives.
	if s.EnableChunking {
		if s.ChunkStrategy == "" {
			s.ChunkStrategy = "paragraph"
		}
		if s.ChunkSize <= 0 {
			s.ChunkSize = 1000
		}
		if s.ChunkOverlap < 0 {
			s.ChunkOverlap = 0
		}
		cfg := ChunkConfig{
			Strategy: ChunkStrategy(s.ChunkStrategy),
			Size:     s.ChunkSize,
			Overlap:  s.ChunkOverlap,
		}
		if err := validateChunkConfig(cfg); err != nil {
			return nil, fmt.Errorf("vectordb-ingest: chunking config invalid: %w", err)
		}
	}
	ctx.Logger().Infof("IngestDocuments initialised: connection=%s provider=%s embeddingProvider=%s model=%s chunking=%v strategy=%s",
		conn.GetName(), conn.GetSettings().DBType, s.EmbeddingProvider, s.EmbeddingModel, s.EnableChunking, s.ChunkStrategy)
	return &Activity{settings: s, conn: conn}, nil
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	l := ctx.Logger()
	l.Debugf("IngestDocuments: starting eval")

	input := &Input{}
	if err := ctx.GetInputObject(input); err != nil {
		return false, fmt.Errorf("vectordb-ingest: %w", err)
	}

	collectionName := input.CollectionName
	if collectionName == "" {
		collectionName = a.settings.DefaultCollection
	}
	if collectionName == "" {
		return false, fmt.Errorf("vectordb-ingest: collectionName is required")
	}

	rawDocs, err := parseDocuments(input.Documents, a.settings.ContentField)
	if err != nil {
		return false, fmt.Errorf("vectordb-ingest: %w", err)
	}

	// Build a files slice from the scalar FileName+FileContent inputs.
	// These are populated by the multipart REST upload flow.
	var fileEntries []interface{}
	if input.FileName != "" && input.FileContent != nil {
		fileEntries = []interface{}{
			map[string]interface{}{
				"name":    input.FileName,
				"content": input.FileContent,
			},
		}
	}

	// Process binary file uploads (PDF, DOCX, TXT, MD).
	fileDocs, err := parseFiles(fileEntries, l)
	if err != nil {
		return false, fmt.Errorf("vectordb-ingest: %w", err)
	}
	rawDocs = append(rawDocs, fileDocs...)

	if len(rawDocs) == 0 {
		return false, fmt.Errorf("vectordb-ingest: at least one document or file is required")
	}

	sourceDocCount := len(rawDocs)
	l.Debugf("IngestDocuments: collection=%s source_doc_count=%d", collectionName, sourceDocCount)

	// ── Optional chunking ────────────────────────────────────────────────────
	// When enabled, each input document is split into smaller segments before
	// embedding. The rawDocs slice is replaced with the expanded chunk slice;
	// all downstream steps (embedding, upsert) are unaware of the split.
	if a.settings.EnableChunking {
		cfg := ChunkConfig{
			Strategy: ChunkStrategy(a.settings.ChunkStrategy),
			Size:     a.settings.ChunkSize,
			Overlap:  a.settings.ChunkOverlap,
		}
		rawDocs = expandChunks(rawDocs, cfg)
		l.Debugf("IngestDocuments: chunking strategy=%s source_docs=%d chunks=%d",
			cfg.Strategy, sourceDocCount, len(rawDocs))
	}

	// OTel trace tags
	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "ingestDocuments")
		tc.SetTag("db.vectordb.provider", a.conn.GetSettings().DBType)
		tc.SetTag("db.vectordb.collection", collectionName)
		tc.SetTag("db.vectordb.source_doc_count", sourceDocCount)
		tc.SetTag("db.vectordb.chunk_count", len(rawDocs))
		tc.SetTag("db.vectordb.chunking_enabled", a.settings.EnableChunking)
		tc.SetTag("db.vectordb.chunk_strategy", a.settings.ChunkStrategy)
		tc.SetTag("db.vectordb.embedding_provider", a.settings.EmbeddingProvider)
		tc.SetTag("db.vectordb.embedding_model", a.settings.EmbeddingModel)
	}

	timeout := a.settings.TimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}
	opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now()

	// -----------------------------------------------------------------------
	// Step 1: Generate embeddings in batches to avoid API payload/rate limits.
	// -----------------------------------------------------------------------
	const defaultEmbeddingBatchSize = 100
	batchSize := a.settings.EmbeddingBatchSize
	if batchSize <= 0 {
		batchSize = defaultEmbeddingBatchSize
	}
	texts := make([]string, len(rawDocs))
	for i, d := range rawDocs {
		texts[i] = d.Text
	}

	allEmbeddings := make([][]float64, 0, len(texts))
	totalTokens := 0
	embDimensions := 0

	for batchStart := 0; batchStart < len(texts); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(texts) {
			batchEnd = len(texts)
		}
		embReq := vdbembed.EmbeddingRequest{
			Provider:   vdbembed.EmbeddingProvider(a.settings.EmbeddingProvider),
			APIKey:     a.settings.EmbeddingAPIKey,
			BaseURL:    a.settings.EmbeddingBaseURL,
			Model:      a.settings.EmbeddingModel,
			Texts:      texts[batchStart:batchEnd],
			Dimensions: a.settings.EmbeddingDimensions,
			InputType:  "search_document", // Cohere: optimise for indexing, not querying
		}
		embResp, embErr := vdbembed.CreateEmbeddings(opCtx, embReq)
		if embErr != nil {
			l.Errorf("IngestDocuments: embedding batch %d-%d failed: collection=%s error=%v",
				batchStart, batchEnd, collectionName, embErr)
			if tc != nil {
				tc.SetTag("error", true)
				tc.LogKV(map[string]interface{}{"event": "error", "message": embErr.Error()})
			}
			if err := ctx.SetOutputObject(&Output{
				Success:  false,
				Error:    fmt.Sprintf("embedding batch [%d-%d] failed: %v", batchStart, batchEnd, embErr),
				Duration: time.Since(start).String(),
			}); err != nil {
				l.Errorf("SetOutputObject: %v", err)
			}
			return false, fmt.Errorf("vectordb-ingest: embedding batch [%d-%d] failed: %w", batchStart, batchEnd, embErr)
		}
		allEmbeddings = append(allEmbeddings, embResp.Embeddings...)
		totalTokens += embResp.TokensUsed
		if embDimensions == 0 {
			embDimensions = embResp.Dimensions
		}
	}

	l.Debugf("IngestDocuments: embedded %d texts in %s dimensions=%d tokens=%d",
		len(rawDocs), time.Since(start), embDimensions, totalTokens)

	// -----------------------------------------------------------------------
	// Step 2: Build vectordb.Document slice — assign IDs and attach vectors.
	// -----------------------------------------------------------------------
	docs := make([]vectordb.Document, len(rawDocs))
	ids := make([]string, len(rawDocs))
	for i, raw := range rawDocs {
		id := raw.ID
		if id == "" {
			id = uuid.NewString() // auto-generate if caller did not provide one
		}
		ids[i] = id

		payload := make(map[string]interface{}, len(raw.Metadata)+1)
		for k, v := range raw.Metadata {
			payload[k] = v
		}
		// Always store the original text in the payload under the content field
		// so ragQuery (and vectorSearch) can retrieve it.
		payload[a.settings.ContentField] = raw.Text

		docs[i] = vectordb.Document{
			ID:      id,
			Vector:  allEmbeddings[i],
			Content: raw.Text,
			Payload: payload,
		}
	}

	// -----------------------------------------------------------------------
	// Step 3: Upsert into VectorDB in batches.
	//
	// The underlying validateUpsertDocuments enforces a hard cap per call
	// (maxUpsertBatchSize = 5_000).  We split here using the same batch size
	// that was used for embeddings so a single EmbeddingBatchSize setting
	// controls both phases and callers never accidentally exceed the cap.
	// -----------------------------------------------------------------------
	for batchStart := 0; batchStart < len(docs); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(docs) {
			batchEnd = len(docs)
		}
		if upsertErr := a.conn.GetClient().UpsertDocuments(opCtx, collectionName, docs[batchStart:batchEnd]); upsertErr != nil {
			l.Errorf("IngestDocuments: upsert batch [%d-%d] failed: collection=%s error=%v",
				batchStart, batchEnd, collectionName, upsertErr)
			if tc != nil {
				tc.SetTag("error", true)
				tc.LogKV(map[string]interface{}{"event": "error", "message": upsertErr.Error()})
			}
			if err := ctx.SetOutputObject(&Output{
				Success:  false,
				Error:    fmt.Sprintf("upsert batch [%d-%d] failed: %v", batchStart, batchEnd, upsertErr),
				Duration: time.Since(start).String(),
			}); err != nil {
				l.Errorf("SetOutputObject: %v", err)
			}
			return false, fmt.Errorf("vectordb-ingest: upsert batch [%d-%d] failed: %w", batchStart, batchEnd, upsertErr)
		}
	}

	duration := time.Since(start)
	l.Debugf("IngestDocuments: success collection=%s ingested=%d dimensions=%d duration=%s",
		collectionName, len(docs), embDimensions, duration)

	if err := ctx.SetOutputObject(&Output{
		Success:             true,
		IngestedCount:       len(docs),
		IDs:                 ids,
		Dimensions:          embDimensions,
		Duration:            duration.String(),
		SourceDocumentCount: sourceDocCount,
		ChunksCreated:       len(docs),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}

// parseFiles converts a files[]interface{} input array into RawDocument slice by
// extracting text from binary documents (PDF, DOCX, TXT, MD).
// Each item must have "name" (string) and "content" (base64 string or []byte).
func parseFiles(files []interface{}, l interface{ Debugf(string, ...interface{}) }) ([]RawDocument, error) {
	if len(files) == 0 {
		return nil, nil
	}
	docs := make([]RawDocument, 0, len(files))
	for idx, item := range files {
		m, ok := toStringMap(item)
		if !ok {
			return nil, fmt.Errorf("files[%d]: must be an object with 'name' and 'content' fields", idx)
		}

		name, _ := m["name"].(string)
		if name == "" {
			name = fmt.Sprintf("file-%d.bin", idx)
		}

		contentRaw, hasContent := m["content"]
		if !hasContent || contentRaw == nil {
			return nil, fmt.Errorf("files[%d] (%s): 'content' field is required", idx, name)
		}

		data, err := fileContentToBytes(contentRaw)
		if err != nil {
			return nil, fmt.Errorf("files[%d] (%s): cannot decode content: %w", idx, name, err)
		}

		text, err := ExtractTextFromBytes(data, name)
		if err != nil {
			return nil, fmt.Errorf("files[%d] (%s): text extraction failed: %w", idx, name, err)
		}
		if text == "" {
			return nil, fmt.Errorf("files[%d] (%s): no text could be extracted — is this a scanned/image PDF?", idx, name)
		}

		l.Debugf("parseFiles: extracted %d chars from %s", len(text), name)

		doc := RawDocument{
			Text: text,
			// Default metadata: source filename and type; caller may override via "metadata" field.
			Metadata: map[string]interface{}{
				"source": name,
				"type":   strings.TrimPrefix(filepath.Ext(name), "."),
			},
		}
		if id, ok := m["id"]; ok && id != nil {
			doc.ID = fmt.Sprintf("%v", id)
		}
		if meta, ok := m["metadata"]; ok {
			if mm, ok := meta.(map[string]interface{}); ok {
				for k, v := range mm {
					doc.Metadata[k] = v
				}
			}
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// toStringMap converts an interface{} to map[string]interface{} if possible.
func toStringMap(v interface{}) (map[string]interface{}, bool) {
	if m, ok := v.(map[string]interface{}); ok {
		return m, true
	}
	return nil, false
}
