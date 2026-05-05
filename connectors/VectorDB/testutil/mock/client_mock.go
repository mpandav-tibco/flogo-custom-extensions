// Package mock provides a testify mock implementation of the VectorDBClient interface
// for use in activity unit tests.
package mock

import (
	"context"

	"github.com/mpandav-tibco/flogo-extensions/vectordb"
	"github.com/stretchr/testify/mock"
)

// Compile-time check: VectorDBClient must implement vectordb.VectorDBClient.
var _ vectordb.VectorDBClient = (*VectorDBClient)(nil)

// VectorDBClient is a mock for vectordb.VectorDBClient.
type VectorDBClient struct {
	mock.Mock
}

func (m *VectorDBClient) CreateCollection(ctx context.Context, cfg vectordb.CollectionConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *VectorDBClient) DeleteCollection(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *VectorDBClient) ListCollections(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func (m *VectorDBClient) CollectionExists(ctx context.Context, name string) (bool, error) {
	args := m.Called(ctx, name)
	return args.Bool(0), args.Error(1)
}

func (m *VectorDBClient) UpsertDocuments(ctx context.Context, collectionName string, docs []vectordb.Document) error {
	args := m.Called(ctx, collectionName, docs)
	return args.Error(0)
}

func (m *VectorDBClient) GetDocument(ctx context.Context, collectionName, id string) (*vectordb.Document, error) {
	args := m.Called(ctx, collectionName, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*vectordb.Document), args.Error(1)
}

func (m *VectorDBClient) DeleteDocuments(ctx context.Context, collectionName string, ids []string) error {
	args := m.Called(ctx, collectionName, ids)
	return args.Error(0)
}

func (m *VectorDBClient) DeleteByFilter(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	args := m.Called(ctx, collectionName, filters)
	return args.Get(0).(int64), args.Error(1)
}

func (m *VectorDBClient) ScrollDocuments(ctx context.Context, req vectordb.ScrollRequest) (*vectordb.ScrollResult, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*vectordb.ScrollResult), args.Error(1)
}

func (m *VectorDBClient) CountDocuments(ctx context.Context, collectionName string, filters map[string]interface{}) (int64, error) {
	args := m.Called(ctx, collectionName, filters)
	return args.Get(0).(int64), args.Error(1)
}

func (m *VectorDBClient) VectorSearch(ctx context.Context, req vectordb.SearchRequest) ([]vectordb.SearchResult, error) {
	args := m.Called(ctx, req)
	return args.Get(0).([]vectordb.SearchResult), args.Error(1)
}

func (m *VectorDBClient) HybridSearch(ctx context.Context, req vectordb.HybridSearchRequest) ([]vectordb.SearchResult, error) {
	args := m.Called(ctx, req)
	return args.Get(0).([]vectordb.SearchResult), args.Error(1)
}

func (m *VectorDBClient) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *VectorDBClient) DBType() string {
	args := m.Called()
	return args.String(0)
}

func (m *VectorDBClient) Close() error {
	args := m.Called()
	return args.Error(0)
}
