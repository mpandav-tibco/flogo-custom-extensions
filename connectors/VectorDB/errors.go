package vectordb

import "fmt"

// Error code constants follow the pattern: VDB-<CATEGORY>-<NUMBER>
const (
	// Configuration errors
	ErrCodeInvalidDBType  = "VDB-CFG-1001"
	ErrCodeMissingHost    = "VDB-CFG-1002"
	ErrCodeInvalidPort    = "VDB-CFG-1003"
	ErrCodeInvalidTimeout = "VDB-CFG-1004"

	// Collection errors
	ErrCodeCollectionNotFound = "VDB-COL-2001"
	ErrCodeCollectionExists   = "VDB-COL-2002"
	ErrCodeInvalidDimensions  = "VDB-COL-2003"
	ErrCodeInvalidMetric      = "VDB-COL-2004"

	// Document errors
	ErrCodeDocumentNotFound  = "VDB-DOC-3001"
	ErrCodeInvalidVector     = "VDB-DOC-3002"
	ErrCodeEmptyDocumentList = "VDB-DOC-3003"
	ErrCodeInvalidDocumentID = "VDB-DOC-3004"
	ErrCodeBatchTooLarge     = "VDB-DOC-3005"

	// Search errors
	ErrCodeInvalidQueryVector = "VDB-SRH-4001"
	ErrCodeInvalidTopK        = "VDB-SRH-4002"
	ErrCodeInvalidAlpha       = "VDB-SRH-4003"
	ErrCodeHybridNotSupported = "VDB-SRH-4004"

	// Connection / provider errors
	ErrCodeConnectionFailed  = "VDB-CON-5001"
	ErrCodeConnectionTimeout = "VDB-CON-5002"
	ErrCodeAuthFailed        = "VDB-CON-5003"
	ErrCodeProviderError     = "VDB-CON-5004"

	// Registry errors
	ErrCodeClientNotFound = "VDB-REG-6001"
	ErrCodeClientExists   = "VDB-REG-6002"
)

// ErrorMessages maps error codes to human-readable descriptions.
var ErrorMessages = map[string]string{
	ErrCodeInvalidDBType:      "DBType must be one of: qdrant, weaviate, chroma, milvus",
	ErrCodeMissingHost:        "Host is required",
	ErrCodeInvalidPort:        "Port must be between 1 and 65535",
	ErrCodeInvalidTimeout:     "TimeoutSeconds must be greater than 0",
	ErrCodeCollectionNotFound: "Collection does not exist",
	ErrCodeCollectionExists:   "Collection already exists",
	ErrCodeInvalidDimensions:  "Dimensions must be greater than 0",
	ErrCodeInvalidMetric:      "DistanceMetric must be one of: cosine, dot, euclidean",
	ErrCodeDocumentNotFound:   "Document not found",
	ErrCodeInvalidVector:      "Vector is nil or empty",
	ErrCodeEmptyDocumentList:  "Document list must not be empty",
	ErrCodeInvalidDocumentID:  "Document ID must not be empty",
	ErrCodeBatchTooLarge:      "Batch exceeds the maximum allowed size",
	ErrCodeInvalidQueryVector: "Query vector is nil or empty",
	ErrCodeInvalidTopK:        "TopK must be greater than 0",
	ErrCodeInvalidAlpha:       "Alpha must be between 0.0 and 1.0",
	ErrCodeHybridNotSupported: "This provider does not support native hybrid search",
	ErrCodeConnectionFailed:   "Failed to establish connection to vector database",
	ErrCodeConnectionTimeout:  "Connection to vector database timed out",
	ErrCodeAuthFailed:         "Authentication failed — check API key / credentials",
	ErrCodeProviderError:      "Vector database provider returned an error",
	ErrCodeClientNotFound:     "No VectorDB client registered with this connectionRef",
	ErrCodeClientExists:       "A VectorDB client is already registered under this connectionRef",
}

// VDBError is a structured, codified error for the VectorDB connector.
type VDBError struct {
	Code    string
	Message string
	Cause   error
}

func (e *VDBError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *VDBError) Unwrap() error { return e.Cause }

// newError creates a VDBError, using the standard message for known codes when msg is empty.
func newError(code, msg string, cause error) *VDBError {
	if msg == "" {
		if m, ok := ErrorMessages[code]; ok {
			msg = m
		}
	}
	return &VDBError{Code: code, Message: msg, Cause: cause}
}
