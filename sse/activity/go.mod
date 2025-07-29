module github.com/milindpandav/flogo-extensions/sse/activity

go 1.21

require (
	github.com/project-flogo/core v1.6.12
	github.com/milindpandav/flogo-extensions/sse v0.0.0
)

replace github.com/milindpandav/flogo-extensions/sse => ../

require (
	github.com/araddon/dateparse v0.0.0-20190622164848-0fb0a474d195 // indirect
	github.com/stretchr/testify v1.8.4 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.24.0 // indirect
)
