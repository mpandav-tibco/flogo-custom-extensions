module github.com/milindpandav/flogo-extensions/sse/trigger

go 1.21

require (
	github.com/project-flogo/core v1.6.12
	github.com/milindpandav/flogo-extensions/sse v0.0.0
)

replace github.com/milindpandav/flogo-extensions/sse => ../

require (
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.7.0 // indirect
	go.uber.org/zap v1.19.1 // indirect
)
