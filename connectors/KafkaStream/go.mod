module github.com/mpandav-tibco/flogo-extensions/kafkastream

go 1.25.0

require (
	github.com/IBM/sarama v1.47.0
	github.com/project-flogo/core v1.6.16
	github.com/stretchr/testify v1.11.1
	github.com/tibco/wi-plugins/contributions/kafka/src/app/Kafka v0.0.0
	golang.org/x/time v0.15.0
)

replace (
	github.com/tibco/wi-contrib => /Users/milindpandav/tmp/tibco.flogo-2.26.0-1798/media/flogo-runtime/wi-contrib
	github.com/tibco/wi-plugins/contributions/kafka/src/app/Kafka => /Users/milindpandav/tmp/tibco.flogo-2.26.0-1798/media/flogo-contributions/wistudio/v1/contributions/Tibco/Kafka
)

require (
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/eapache/go-resiliency v1.7.0 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20250401214520-65e299d6c5c9 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/tibco/wi-contrib v3.2.0+incompatible // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.24.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/oauth2 v0.8.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
