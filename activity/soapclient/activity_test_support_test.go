package soapclient_test

import (
	"io"
	"os"

	soapclient "github.com/mpandav-tibco/flogo-custom-extensions/activity/soapclient"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data"
	"github.com/project-flogo/core/data/mapper"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/connection"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
)

// ---------------------------------------------------------------------------
// Test harness helpers
// ---------------------------------------------------------------------------

// activityConfig holds all settings options used to construct a test Activity.
type activityConfig struct {
	endpoint            string
	soapVersion         string
	xmlMode             bool
	xmlAttributePrefix  string
	timeout             int
	wsdlUrl             string // sets wsdlSourceType="WSDL File" automatically
	wsdlHttpUrl         string // sets wsdlSourceType="WSDL URL" automatically
	wsdlOp              string
	autoUseWsdlEndpoint bool
	skipTlsVerify       bool               // set true when hitting httptest TLS servers
	authorization       bool               // set true to enable HTTP auth header injection
	authConn            connection.Manager // mock connection; passed directly to New() via settings
}

// evalInput holds the runtime inputs for a single Eval call in tests.
type evalInput struct {
	soapAction  string
	requestBody interface{}
	headers     interface{}
}

// newTestActivity creates a soapclient Activity using test settings.
func newTestActivity(cfg activityConfig) (activity.Activity, error) {
	if cfg.soapVersion == "" {
		cfg.soapVersion = "1.1"
	}
	if cfg.timeout == 0 {
		cfg.timeout = 10
	}

	// Derive wsdlSourceType from which wsdl field is set.
	wsdlSourceType := "None"
	if cfg.wsdlHttpUrl != "" {
		wsdlSourceType = "WSDL URL"
	} else if cfg.wsdlUrl != "" {
		wsdlSourceType = "WSDL File"
	}

	settings := map[string]interface{}{
		"soapServiceEndpoint": cfg.endpoint,
		"soapVersion":         cfg.soapVersion,
		"xmlMode":             cfg.xmlMode,
		"xmlAttributePrefix":  cfg.xmlAttributePrefix,
		"timeout":             cfg.timeout,
		"enableTLS":           false,
		"skipTlsVerify":       cfg.skipTlsVerify,
		"wsdlSourceType":      wsdlSourceType,
		"wsdlUrl":             cfg.wsdlUrl,
		"wsdlHttpUrl":         cfg.wsdlHttpUrl,
		"wsdlOperation":       cfg.wsdlOp,
		"autoUseWsdlEndpoint": cfg.autoUseWsdlEndpoint,
		"authorization":       cfg.authorization,
		// authorizationConn is passed as connection.Manager so coerce.ToConnection
		// accepts it directly (it handles connection.Manager as a first-class case).
		// When nil, New() skips auth loading via its nil guard.
		"authorizationConn": cfg.authConn,
	}

	ctx := &testInitContext{settings: settings}
	return soapclient.New(ctx)
}

// evalActivity calls Eval on the activity with the given inputs and returns the Output.
func evalActivity(act activity.Activity, in evalInput) (*soapclient.Output, error) {
	ctx := &testContext{
		inputs: map[string]interface{}{
			"soapAction":         in.soapAction,
			"soapRequestBody":    in.requestBody,
			"soapRequestHeaders": in.headers,
		},
		outputs: make(map[string]interface{}),
	}
	_, err := act.Eval(ctx)
	if err != nil {
		return nil, err
	}

	out := &soapclient.Output{}
	_ = metadata.MapToStruct(ctx.outputs, out, true)
	return out, nil
}

// ---------------------------------------------------------------------------
// Minimal InitContext implementation
// ---------------------------------------------------------------------------

type testInitContext struct {
	settings map[string]interface{}
}

func (c *testInitContext) Settings() map[string]interface{} { return c.settings }
func (c *testInitContext) Logger() log.Logger               { return log.ChildLogger(log.RootLogger(), "test") }
func (c *testInitContext) Name() string                     { return "test-soapclient" }
func (c *testInitContext) MapperFactory() mapper.Factory    { return nil }
func (c *testInitContext) HostName() string                 { return "test-host" }

// ---------------------------------------------------------------------------
// Minimal activity.Context implementation
// ---------------------------------------------------------------------------

type testContext struct {
	inputs  map[string]interface{}
	outputs map[string]interface{}
}

func (c *testContext) ActivityHost() activity.Host { return nil }
func (c *testContext) Name() string                { return "test" }

func (c *testContext) GetInput(name string) interface{} {
	return c.inputs[name]
}

func (c *testContext) SetOutput(name string, value interface{}) error {
	c.outputs[name] = value
	return nil
}

func (c *testContext) GetInputObject(obj data.StructValue) error {
	return metadata.MapToStruct(c.inputs, obj, true)
}

func (c *testContext) SetOutputObject(obj data.StructValue) error {
	m := metadata.StructToMap(obj)
	for k, v := range m {
		c.outputs[k] = v
	}
	return nil
}

func (c *testContext) GetTracingContext() trace.TracingContext { return nil }

func (c *testContext) GetSharedTempData() map[string]interface{} { return nil }
func (c *testContext) Logger() log.Logger {
	return log.ChildLogger(log.RootLogger(), "test")
}

// ---------------------------------------------------------------------------
// io.ReadFile shim (available in Go 1.16+; kept for explicitness in tests)
// ---------------------------------------------------------------------------

var _ = io.ReadAll  // ensure io is used
var _ = os.ReadFile // ensure os is used
