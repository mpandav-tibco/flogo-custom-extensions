module github.com/milindpandav/flogo-extensions/vectordb/activity/rerank

go 1.22.2

toolchain go1.24.3

require github.com/project-flogo/core v1.6.18

require (
	github.com/araddon/dateparse v0.0.0-20190622164848-0fb0a474d195 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.18.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/milindpandav/flogo-extensions/vectordb => ../../
	github.com/milindpandav/flogo-extensions/vectordb/connector => ../../connector
)
