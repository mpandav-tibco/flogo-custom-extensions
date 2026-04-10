module github.com/milindpandav/flogo-extensions/kafka-stream/test/integration

go 1.24.0

toolchain go1.24.3

replace (
	github.com/milindpandav/flogo-extensions/kafka-stream => ../../../
	github.com/milindpandav/flogo-extensions/kafka-stream/activity/aggregate => ../../aggregate
	github.com/milindpandav/flogo-extensions/kafka-stream/activity/filter => ../../filter
)

require (
	github.com/IBM/sarama v1.46.3
	github.com/milindpandav/flogo-extensions/kafka-stream v0.0.0
	github.com/milindpandav/flogo-extensions/kafka-stream/activity/aggregate v0.0.0-00010101000000-000000000000
	github.com/milindpandav/flogo-extensions/kafka-stream/activity/filter v0.0.0-00010101000000-000000000000
	github.com/project-flogo/core v1.6.17
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/araddon/dateparse v0.0.0-20190622164848-0fb0a474d195 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/eapache/go-resiliency v1.7.0 // indirect
	github.com/eapache/go-xerial-snappy v0.0.0-20230731223053-c322873962e3 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/klauspost/compress v1.18.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20250401214520-65e299d6c5c9 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	go.uber.org/atomic v1.6.0 // indirect
	go.uber.org/multierr v1.5.0 // indirect
	go.uber.org/zap v1.16.0 // indirect
	golang.org/x/crypto v0.43.0 // indirect
	golang.org/x/net v0.46.0 // indirect
	golang.org/x/tools v0.26.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
