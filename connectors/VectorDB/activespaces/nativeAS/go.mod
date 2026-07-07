module github.com/mpandav-tibco/flogo-extensions/VectorDB/activespaces/nativeAS

go 1.24.1

require (
	github.com/google/uuid v1.6.0
	github.com/project-flogo/core v1.6.18
	tibco.com/tibdg v0.0.0-00010101000000-000000000000
)

// The TIBCO ActiveSpaces native Go client (CGO). Vendored under dep/ following
// the OOTB ActiveSpaces connector pattern. It links libtibdg/libtib/libtibutil,
// so this connector builds only with CGO + the AS SDK present (see README).
replace tibco.com/tibdg => ./dep/tibco.com/tibdg

require (
	github.com/araddon/dateparse v0.0.0-20190622164848-0fb0a474d195 // indirect
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
)
