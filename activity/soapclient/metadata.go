package soapclient

import (
	"github.com/project-flogo/core/data/coerce"
)

// ---------------------------------------------------------------------------
// Settings — configured once at activity initialisation
// ---------------------------------------------------------------------------

// Settings holds all compile-time configuration for the SOAP Client activity.
type Settings struct {
	// ── Core ──────────────────────────────────────────────────────────────
	SoapServiceEndpoint string `md:"soapServiceEndpoint"` // Target SOAP endpoint URL
	SoapVersion         string `md:"soapVersion"`         // "1.1" | "1.2"
	Timeout             int    `md:"timeout"`             // HTTP timeout in seconds
	XMLMode             bool   `md:"xmlMode"`             // true = JSON object in (converted to XML), raw XML string out
	XMLAttributePrefix  string `md:"xmlAttributePrefix"`  // prefix for attrs in JSON↔XML

	// ── TLS ───────────────────────────────────────────────────────────────
	EnableTLS         bool   `md:"enableTLS"`
	ServerCertificate string `md:"serverCertificate"`
	ClientCertificate string `md:"clientCertificate"`
	ClientKey         string `md:"clientKey"`

	// ── WSDL ──────────────────────────────────────────────────────────────
	// WSDLSourceType controls which WSDL input the user chose:
	//   "None"      – no WSDL (default)
	//   "WSDL File" – use WSDLUrl (fileselector value)
	//   "WSDL URL"  – use WSDLHttpUrl (plain HTTP/HTTPS string)
	WSDLSourceType string `md:"wsdlSourceType"`

	// WSDLHttpUrl is an optional HTTP/HTTPS URL for fetching the WSDL remotely.
	WSDLHttpUrl string `md:"wsdlHttpUrl"`

	// WSDLUrl is the location of the WSDL document. Supported forms:
	//   - HTTP/HTTPS URL:  "https://host/svc?wsdl"
	//   - File path:       "/opt/services/svc.wsdl"
	//   - file:// URI:     "file:///opt/services/svc.wsdl"
	// Leave empty to skip WSDL integration (behaves like the original activity).
	WSDLUrl string `md:"wsdlUrl"`

	// WSDLOperation is the name of the WSDL operation to invoke.
	// Required when WSDLUrl is set. Used to:
	//   - Auto-inject the SOAPAction header when soapAction input is empty.
	//   - Generate the request body skeleton (stored in WSDLBodySkeleton).
	WSDLOperation string `md:"wsdlOperation"`

	// AutoUseWSDLEndpoint, when true, overrides SoapServiceEndpoint with the
	// <soap:address location> value found in the WSDL <service> element.
	AutoUseWSDLEndpoint bool `md:"autoUseWsdlEndpoint"`

	// SkipTLSVerify disables TLS certificate verification. Default is false (secure).
	// WARNING: setting true makes HTTPS connections vulnerable to MitM attacks.
	// Use only for development or self-signed-cert environments.
	SkipTLSVerify bool `md:"skipTlsVerify"`

	// MaxResponseBodyMB limits the response body size in megabytes. Responses
	// larger than this are rejected with an error. Default is 10 MiB when zero.
	MaxResponseBodyMB int `md:"maxResponseBodyMB"`

	// MaxConnsPerHost limits the number of simultaneous active HTTP connections
	// to the SOAP endpoint host. Default is 100 when zero or unset.
	// Increase this for high-concurrency flows that send many parallel SOAP calls
	// to the same endpoint.
	MaxConnsPerHost int `md:"maxConnsPerHost"`

	// ── Authorization ─────────────────────────────────────────────────────
	// Authorization enables HTTP authentication via the selected HTTP auth connector.
	Authorization bool `md:"authorization"`

	// Note: AuthorizationConn (connection.Manager) is NOT in this struct.
	// metadata.MapToStruct cannot safely coerce a nil settings value to
	// connection.Manager — it falls into coerce.ToConnection's default case
	// and returns an error. The connection is loaded manually in New() with a
	// nil guard and stored directly on the Activity struct.
}

// ---------------------------------------------------------------------------
// Input — provided at runtime for each flow invocation
// ---------------------------------------------------------------------------

// Input holds all runtime inputs for the SOAP Client activity.
type Input struct {
	// SoapAction overrides the SOAPAction HTTP header.
	// When WSDLOperation is configured and this field is empty, the activity
	// automatically injects the soapAction from the WSDL binding.
	SoapAction string `md:"soapAction"`

	// HttpQueryParams are appended to the endpoint URL as query parameters.
	HttpQueryParams map[string]string `md:"httpQueryParams"`

	// SOAPRequestHeaders are the SOAP <Header> contents.
	// In xmlMode=true: XML string.
	// In xmlMode=false: JSON object (will be converted to XML).
	SOAPRequestHeaders interface{} `md:"soapRequestHeaders"`

	// SOAPRequestBody is the SOAP <Body> contents.
	// In xmlMode=true: XML string.
	// In xmlMode=false: JSON object (will be converted to XML).
	SOAPRequestBody interface{} `md:"soapRequestBody"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"soapAction":         i.SoapAction,
		"httpQueryParams":    i.HttpQueryParams,
		"soapRequestHeaders": i.SOAPRequestHeaders,
		"soapRequestBody":    i.SOAPRequestBody,
	}
}

func (i *Input) FromMap(values map[string]interface{}) error {
	i.SOAPRequestHeaders = values["soapRequestHeaders"]
	i.SOAPRequestBody = values["soapRequestBody"]
	i.HttpQueryParams, _ = coerce.ToParams(values["httpQueryParams"])
	i.SoapAction, _ = values["soapAction"].(string)
	return nil
}

// ---------------------------------------------------------------------------
// Output — produced after each invocation
// ---------------------------------------------------------------------------

// Output holds all outputs set after a SOAP call completes.
type Output struct {
	HttpStatus          int         `md:"httpStatus"`
	IsFault             bool        `md:"isFault"`
	SOAPResponsePayload interface{} `md:"soapResponsePayload"`
	SOAPResponseHeaders interface{} `md:"soapResponseHeaders"`
	SOAPResponseFault   interface{} `md:"soapResponseFault"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"httpStatus":          o.HttpStatus,
		"isFault":             o.IsFault,
		"soapResponsePayload": o.SOAPResponsePayload,
		"soapResponseHeaders": o.SOAPResponseHeaders,
		"soapResponseFault":   o.SOAPResponseFault,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	o.IsFault, _ = values["isFault"].(bool)
	o.SOAPResponsePayload = values["soapResponsePayload"]
	o.SOAPResponseHeaders = values["soapResponseHeaders"]
	o.SOAPResponseFault = values["soapResponseFault"]
	o.HttpStatus, _ = values["httpStatus"].(int)
	return nil
}
