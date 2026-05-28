package chromalocal

// Methods on *Embedded — all return errNotSupported since NewEmbedded/StartEmbedded
// already return an error before an *Embedded can be obtained.

func (e *Embedded) Heartbeat() (uint64, error)    { return 0, errNotSupported }
func (e *Embedded) MaxBatchSize() (uint32, error) { return 0, errNotSupported }
func (e *Embedded) Reset() error                  { return errNotSupported }
func (e *Embedded) Stop() error                   { return errNotSupported }
func (e *Embedded) Close() error                  { return errNotSupported }

func (e *Embedded) Healthcheck() (*EmbeddedHealthCheckResponse, error) {
	return nil, errNotSupported
}
func (e *Embedded) IndexingStatus(_ EmbeddedIndexingStatusRequest) (*EmbeddedIndexingStatusResponse, error) {
	return nil, errNotSupported
}

func (e *Embedded) CreateTenant(_ EmbeddedCreateTenantRequest) error { return errNotSupported }
func (e *Embedded) GetTenant(_ EmbeddedGetTenantRequest) (*EmbeddedTenant, error) {
	return nil, errNotSupported
}
func (e *Embedded) UpdateTenant(_ EmbeddedUpdateTenantRequest) error { return errNotSupported }

func (e *Embedded) CreateDatabase(_ EmbeddedCreateDatabaseRequest) error { return errNotSupported }
func (e *Embedded) ListDatabases(_ EmbeddedListDatabasesRequest) ([]EmbeddedDatabase, error) {
	return nil, errNotSupported
}
func (e *Embedded) GetDatabase(_ EmbeddedGetDatabaseRequest) (*EmbeddedDatabase, error) {
	return nil, errNotSupported
}
func (e *Embedded) DeleteDatabase(_ EmbeddedDeleteDatabaseRequest) error { return errNotSupported }

func (e *Embedded) CreateCollection(_ EmbeddedCreateCollectionRequest) (*EmbeddedCollection, error) {
	return nil, errNotSupported
}
func (e *Embedded) GetCollection(_ EmbeddedGetCollectionRequest) (*EmbeddedCollection, error) {
	return nil, errNotSupported
}
func (e *Embedded) ListCollections(_ EmbeddedListCollectionsRequest) ([]EmbeddedCollection, error) {
	return nil, errNotSupported
}
func (e *Embedded) CountCollections(_ EmbeddedCountCollectionsRequest) (uint32, error) {
	return 0, errNotSupported
}
func (e *Embedded) UpdateCollection(_ EmbeddedUpdateCollectionRequest) error { return errNotSupported }
func (e *Embedded) DeleteCollection(_ EmbeddedDeleteCollectionRequest) error { return errNotSupported }
func (e *Embedded) ForkCollection(_ EmbeddedForkCollectionRequest) (*EmbeddedCollection, error) {
	return nil, errNotSupported
}

func (e *Embedded) Add(_ EmbeddedAddRequest) error                     { return errNotSupported }
func (e *Embedded) UpsertRecords(_ EmbeddedUpsertRecordsRequest) error { return errNotSupported }
func (e *Embedded) UpdateRecords(_ EmbeddedUpdateRecordsRequest) error { return errNotSupported }
func (e *Embedded) DeleteRecords(_ EmbeddedDeleteRecordsRequest) error { return errNotSupported }
func (e *Embedded) GetRecords(_ EmbeddedGetRecordsRequest) (*EmbeddedGetRecordsResponse, error) {
	return nil, errNotSupported
}
func (e *Embedded) CountRecords(_ EmbeddedCountRecordsRequest) (uint32, error) {
	return 0, errNotSupported
}
func (e *Embedded) Query(_ EmbeddedQueryRequest) (*EmbeddedQueryResponse, error) {
	return nil, errNotSupported
}
