module github.com/mpandav-tibco/flogo-extensions/vectordb-chroma

go 1.25

// Replace chroma-go-local with a no-CGo stub. chroma-go/pkg/api/v2 imports this
// package for its in-process (PersistentClient) code path, which we never use.
// The real chroma-go-local pulls in ebitengine/purego → runtime/cgo, breaking
// gopls and CGO_ENABLED=0 builds. The stub provides the same compile-time API
// surface without any C dependencies.
replace github.com/amikos-tech/chroma-go-local => ./_stubs/chroma-go-local

// Replace pure-onnx with a no-CGo stub. chroma-go/pkg/embeddings/default_ef imports
// pure-onnx/ort and pure-onnx/embeddings/minilm unconditionally. pure-onnx pulls in
// ebitengine/purego → runtime/cgo, breaking gopls and CGO_ENABLED=0 builds.
replace github.com/amikos-tech/pure-onnx => ./_stubs/pure-onnx

toolchain go1.25.9

require (
	github.com/amikos-tech/chroma-go v0.4.0
	github.com/google/uuid v1.6.0
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
	github.com/project-flogo/core v1.6.18
	github.com/stretchr/testify v1.11.1
)

require (
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/araddon/dateparse v0.0.0-20190622164848-0fb0a474d195 // indirect
	google.golang.org/grpc v1.79.3 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

require (
	github.com/amikos-tech/chroma-go-local v0.3.3 // indirect
	github.com/amikos-tech/pure-onnx v0.0.1 // indirect
	github.com/creasty/defaults v1.8.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/gabriel-vasile/mimetype v1.4.12 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260209200024-4cfbd4190f57 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
