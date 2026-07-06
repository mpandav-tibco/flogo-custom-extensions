package ingestDocuments

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	vectordb "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch"
	vectordbconnector "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch/connector"
	vdbembed "github.com/mpandav-tibco/flogo-extensions/vectordb-elasticsearch/embeddings"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() { _ = activity.Register(&Activity{}, New) }

// Activity combines embedding generation and VectorDB upsert into one step.
type Activity struct {
	settings *Settings
	conn     *vectordbconnector.ElasticsearchConnection
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
	conn, ok := s.Connection.GetConnection().(*vectordbconnector.ElasticsearchConnection)
	if !ok {
		return nil, fmt.Errorf("vectordb-ingest: invalid connection type, expected *ElasticsearchConnection")
	}

	// Inherit embedding provider settings from connector when not set.
	cs := conn.GetSettings()
	if cs.EnableEmbedding {
		if s.EmbeddingProvider == "" {
			s.EmbeddingProvider = cs.EmbeddingProvider
		}
		if s.EmbeddingAPIKey == "" {
			s.EmbeddingAPIKey = cs.EmbeddingAPIKey
		}
		if s.EmbeddingBaseURL == "" {
			s.EmbeddingBaseURL = cs.EmbeddingBaseURL
		}
	}

	if s.ChunkStrategy == "" {
		s.ChunkStrategy = "paragraph"
	}
	if s.ChunkSize <= 0 {
		s.ChunkSize = 1000
	}
	if s.ChunkOverlap < 0 {
		s.ChunkOverlap = 0
	}
	if s.BatchSize <= 0 {
		s.BatchSize = 100
	}

	ctx.Logger().Infof("IngestDocuments initialised: connection=%s embeddingProvider=%s embeddingModel=%s chunkStrategy=%s",
		conn.GetName(), s.EmbeddingProvider, s.EmbeddingModel, s.ChunkStrategy)
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

	tc := ctx.GetTracingContext()
	if tc != nil {
		tc.SetTag("db.system", "vectordb")
		tc.SetTag("db.operation", "ingestDocuments")
		tc.SetTag("db.vectordb.provider", "elasticsearch")
		tc.SetTag("db.vectordb.collection", collectionName)
	}

	start := time.Now()

	var rawTexts []string
	var rawIDs []string

	// File upload path
	if input.FileContent != "" {
		fileName := input.FileName
		if fileName == "" {
			fileName = "upload.bin"
		}
		ext := strings.ToLower(filepath.Ext(fileName))
		text, err := parseFile(ext, []byte(input.FileContent))
		if err != nil {
			return false, fmt.Errorf("vectordb-ingest: file parse error: %w", err)
		}
		chunks := expandChunks([]string{text}, a.settings.ChunkStrategy, a.settings.ChunkSize, a.settings.ChunkOverlap)
		rawTexts = append(rawTexts, chunks...)
	}

	// Inline documents path
	for _, rawDoc := range input.Documents {
		docMap, ok := rawDoc.(map[string]interface{})
		if !ok {
			continue
		}
		text := ""
		if c, ok := docMap["content"]; ok {
			text = fmt.Sprintf("%v", c)
		}
		if text == "" {
			if c, ok := docMap["text"]; ok {
				text = fmt.Sprintf("%v", c)
			}
		}
		if text == "" {
			continue
		}
		id := ""
		if d, ok := docMap["id"]; ok {
			id = fmt.Sprintf("%v", d)
		}
		chunks := expandChunks([]string{text}, a.settings.ChunkStrategy, a.settings.ChunkSize, a.settings.ChunkOverlap)
		for range chunks {
			rawIDs = append(rawIDs, id)
		}
		rawTexts = append(rawTexts, chunks...)
	}

	if len(rawTexts) == 0 {
		return false, fmt.Errorf("vectordb-ingest: no text content found in input")
	}

	l.Debugf("IngestDocuments: collection=%s chunks=%d", collectionName, len(rawTexts))

	var allEmbeddings [][]float64
	totalTokens := 0
	embDimensions := 0

	if a.settings.EmbeddingProvider != "" && a.settings.EmbeddingModel != "" {
		connSettings := a.conn.GetSettings()
		embedTimeout := connSettings.TimeoutSeconds * 2
		if embedTimeout <= 0 {
			embedTimeout = 120
		}
		embedCtx, embedCancel := context.WithTimeout(ctx.GoContext(), time.Duration(embedTimeout)*time.Second)
		defer embedCancel()

		batchSize := a.settings.BatchSize
		if batchSize <= 0 {
			batchSize = 100
		}

		for batchStart := 0; batchStart < len(rawTexts); batchStart += batchSize {
			batchEnd := batchStart + batchSize
			if batchEnd > len(rawTexts) {
				batchEnd = len(rawTexts)
			}

			embResp, embErr := vdbembed.CreateEmbeddings(embedCtx, vdbembed.EmbeddingRequest{
				Provider:  vdbembed.EmbeddingProvider(a.settings.EmbeddingProvider),
				APIKey:    a.settings.EmbeddingAPIKey,
				BaseURL:   a.settings.EmbeddingBaseURL,
				Model:     a.settings.EmbeddingModel,
				Texts:     rawTexts[batchStart:batchEnd],
				InputType: "search_document",
			})
			if embErr != nil {
				l.Errorf("IngestDocuments: embedding batch [%d-%d] error=%v", batchStart, batchEnd, embErr)
				if tc != nil {
					tc.SetTag("error", true)
				}
				if err2 := ctx.SetOutputObject(&Output{
					Success:  false,
					Error:    fmt.Sprintf("embedding batch [%d-%d] failed: %v", batchStart, batchEnd, embErr),
					Duration: time.Since(start).String(),
				}); err2 != nil {
					l.Errorf("SetOutputObject: %v", err2)
				}
				return true, nil
			}
			allEmbeddings = append(allEmbeddings, embResp.Embeddings...)
			totalTokens += embResp.TokensUsed
			if embDimensions == 0 {
				embDimensions = embResp.Dimensions
			}
		}
		l.Debugf("IngestDocuments: embedded %d texts dims=%d tokens=%d", len(rawTexts), embDimensions, totalTokens)
		_ = totalTokens
	}

	docs := make([]vectordb.Document, len(rawTexts))
	ids := make([]string, len(rawTexts))
	for i, text := range rawTexts {
		id := ""
		if i < len(rawIDs) {
			id = rawIDs[i]
		}
		if id == "" {
			id = uuid.NewString()
		}
		ids[i] = id

		doc := vectordb.Document{
			ID:      id,
			Content: text,
			Payload: map[string]interface{}{
				"text":   text,
				"source": input.FileName,
			},
		}
		if i < len(allEmbeddings) {
			doc.Vector = allEmbeddings[i]
		}
		docs[i] = doc
	}

	connSettings := a.conn.GetSettings()
	opTimeout := connSettings.TimeoutSeconds
	if opTimeout <= 0 {
		opTimeout = 60
	}

	batchSize := a.settings.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	for offset := 0; offset < len(docs); offset += batchSize {
		end := offset + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[offset:end]

		opCtx, cancel := context.WithTimeout(ctx.GoContext(), time.Duration(opTimeout)*time.Second)
		if err := a.conn.GetClient().UpsertDocuments(opCtx, collectionName, batch); err != nil {
			cancel()
			l.Errorf("IngestDocuments: upsert batch [%d-%d] error=%v", offset, end, err)
			if tc != nil {
				tc.SetTag("error", true)
			}
			if err2 := ctx.SetOutputObject(&Output{
				Success:  false,
				Error:    fmt.Sprintf("upsert batch [%d-%d] failed: %v", offset, end, err),
				Duration: time.Since(start).String(),
			}); err2 != nil {
				l.Errorf("SetOutputObject: %v", err2)
			}
			return true, nil
		}
		cancel()
		l.Debugf("IngestDocuments: upserted batch [%d-%d]", offset, end)
	}

	duration := time.Since(start)
	l.Infof("IngestDocuments: collection=%s ingested=%d dims=%d duration=%s", collectionName, len(docs), embDimensions, duration)

	if tc != nil {
		tc.SetTag("db.vectordb.ingested_count", len(docs))
	}

	idsInterface := make([]interface{}, len(ids))
	for i, id := range ids {
		idsInterface[i] = id
	}

	if err := ctx.SetOutputObject(&Output{
		Success:       true,
		IngestedCount: len(docs),
		IDs:           idsInterface,
		Duration:      duration.String(),
	}); err != nil {
		l.Errorf("SetOutputObject: %v", err)
	}
	return true, nil
}

// parseFile dispatches to the appropriate text extractor based on file extension.
func parseFile(ext string, data []byte) (string, error) {
	switch ext {
	case ".pdf":
		return ExtractTextFromBytes(data, "pdf")
	case ".docx":
		return ExtractTextFromBytes(data, "docx")
	case ".txt", ".md", ".csv", ".json", "":
		return string(data), nil
	default:
		return string(data), nil
	}
}
