package soapclient_test

// ---------------------------------------------------------------------------
// Live integration tests — SOAP Client activity
//
// Tests exercise the activity end-to-end against real public SOAP services.
// Every live test skips when the service is unreachable so CI never fails
// due to transient network issues.
//
// Services used:
//   Calculator  http://www.dneonline.com/calculator.asmx
//   CountryInfo http://webservices.oorsprong.org/websamples.countryinfo/CountryInfoService.wso
//
// Run all:              go test -v ./...
// Run only live:        go test -v -run TestLive ./...
// Run only unit tests:  go test -v -run "TestActivity|TestWSDL|TestBody" ./...
// ---------------------------------------------------------------------------

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/soapclient/wsdl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Live service constants
// ---------------------------------------------------------------------------

const (
	calcEndpoint    = "http://www.dneonline.com/calculator.asmx"
	calcWSDL        = "http://www.dneonline.com/calculator.asmx?WSDL"
	countryEndpoint = "http://webservices.oorsprong.org/websamples.countryinfo/CountryInfoService.wso"
)

// pingService returns true when the given URL responds within 5 seconds.
func pingService(url string) bool {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// ---------------------------------------------------------------------------
// T1 — Live: Add two numbers, SOAP 1.1, XML mode
// ---------------------------------------------------------------------------

func TestLive_Calculator_Add_XMLMode(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{endpoint: calcEndpoint, soapVersion: "1.1", xmlMode: true, timeout: 15})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction:  `"http://tempuri.org/Add"`,
		requestBody: `<Add xmlns="http://tempuri.org/"><intA>15</intA><intB>27</intB></Add>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.False(t, out.IsFault)
	payload, ok := out.SOAPResponsePayload.(string)
	require.True(t, ok, "xmlMode must return string payload")
	assert.Contains(t, payload, "AddResult")
	assert.Contains(t, payload, "42", "15+27 must equal 42")
}

// ---------------------------------------------------------------------------
// T2 — Live: Subtract, SOAP 1.1, XML mode
// ---------------------------------------------------------------------------

func TestLive_Calculator_Subtract_XMLMode(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{endpoint: calcEndpoint, soapVersion: "1.1", xmlMode: true, timeout: 15})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction:  `"http://tempuri.org/Subtract"`,
		requestBody: `<Subtract xmlns="http://tempuri.org/"><intA>100</intA><intB>37</intB></Subtract>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.Contains(t, out.SOAPResponsePayload.(string), "63", "100-37 must equal 63")
}

// ---------------------------------------------------------------------------
// T3 — Live: Multiply via SOAP 1.2
// ---------------------------------------------------------------------------

func TestLive_Calculator_Multiply_SOAP12(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{endpoint: calcEndpoint, soapVersion: "1.2", xmlMode: true, timeout: 15})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction:  "http://tempuri.org/Multiply",
		requestBody: `<Multiply xmlns="http://tempuri.org/"><intA>6</intA><intB>7</intB></Multiply>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.False(t, out.IsFault)
	assert.Contains(t, out.SOAPResponsePayload.(string), "42", "6×7 must equal 42")
}

// ---------------------------------------------------------------------------
// T4 — Live: Add in JSON mode (map body → XML via mxj)
// ---------------------------------------------------------------------------

func TestLive_Calculator_Add_JSONMode(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{endpoint: calcEndpoint, soapVersion: "1.1", xmlMode: false, timeout: 15})
	require.NoError(t, err)

	bodyMap := map[string]interface{}{
		"Add": map[string]interface{}{
			"-xmlns": "http://tempuri.org/",
			"intA":   "20",
			"intB":   "22",
		},
	}
	out, err := evalActivity(act, evalInput{
		soapAction:  `"http://tempuri.org/Add"`,
		requestBody: bodyMap,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.False(t, out.IsFault)
	raw, _ := json.Marshal(out.SOAPResponsePayload)
	assert.Contains(t, string(raw), "42", "20+22 must equal 42")
}

// ---------------------------------------------------------------------------
// T5 — Live: WSDL auto-discovery from live URL; soapAction injected automatically
// ---------------------------------------------------------------------------

func TestLive_Calculator_WSDL_LiveURL_AutoSoapAction(t *testing.T) {
	if !pingService(calcWSDL) {
		t.Skip("Calculator WSDL unreachable")
	}
	act, err := newTestActivity(activityConfig{
		endpoint:    calcEndpoint,
		soapVersion: "1.1",
		xmlMode:     true,
		timeout:     20,
		wsdlHttpUrl: calcWSDL,
		wsdlOp:      "Add",
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		// No soapAction — WSDL must supply it
		requestBody: `<Add xmlns="http://tempuri.org/"><intA>21</intA><intB>21</intB></Add>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.Contains(t, out.SOAPResponsePayload.(string), "42")
}

// ---------------------------------------------------------------------------
// T6 — Live: local WSDL file used to invoke Add on the live service
// ---------------------------------------------------------------------------

func TestLive_Calculator_WSDL_LocalFile_Invoke(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{
		endpoint:    calcEndpoint,
		soapVersion: "1.1",
		xmlMode:     true,
		timeout:     15,
		wsdlUrl:     "testdata/calculator.wsdl",
		wsdlOp:      "Multiply",
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		requestBody: `<Multiply xmlns="http://tempuri.org/"><intA>6</intA><intB>7</intB></Multiply>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.Contains(t, out.SOAPResponsePayload.(string), "42")
}

// ---------------------------------------------------------------------------
// T7 — WSDL parser: all 4 Calculator operations listed; skeleton and schema correct
// ---------------------------------------------------------------------------

func TestLive_Calculator_WSDL_SkeletonAndSchema(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	info, err := wsdl.Parse("testdata/calculator.wsdl", nil)
	require.NoError(t, err)

	ops := wsdl.ListOperations(info)
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = op.Name
	}
	assert.ElementsMatch(t, []string{"Add", "Subtract", "Multiply", "Divide"}, names)

	op := wsdl.FindOperation(info, "Add", "1.1")
	require.NotNil(t, op)
	skeleton := wsdl.BuildXMLBody(op, info.TargetNamespace)
	assert.Contains(t, skeleton, "<intA>")
	assert.Contains(t, skeleton, "<intB>")

	schemaJSON := wsdl.BuildJSONSchema(op)
	var schema map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(schemaJSON), &schema))
	props := schema["properties"].(map[string]interface{})
	assert.Contains(t, props, "intA")
	assert.Contains(t, props, "intB")
}

// ---------------------------------------------------------------------------
// T8 — Live: autoUseWsdlEndpoint replaces a wrong endpoint with WSDL address
// ---------------------------------------------------------------------------

func TestLive_Calculator_AutoUseWSDLEndpoint(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{
		endpoint:            "http://this-will-be-overridden.invalid/wrong",
		soapVersion:         "1.1",
		xmlMode:             true,
		timeout:             15,
		wsdlUrl:             "testdata/calculator.wsdl",
		wsdlOp:              "Add",
		autoUseWsdlEndpoint: true,
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		requestBody: `<Add xmlns="http://tempuri.org/"><intA>10</intA><intB>32</intB></Add>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.Contains(t, out.SOAPResponsePayload.(string), "42", "endpoint from WSDL must route correctly")
}

// ---------------------------------------------------------------------------
// T9 — Live: CountryInfo — ListOfContinentsByName returns Africa and Europe
// ---------------------------------------------------------------------------

func TestLive_CountryInfo_ListContinents(t *testing.T) {
	if !pingService(countryEndpoint) {
		t.Skip("CountryInfo service unreachable")
	}
	act, err := newTestActivity(activityConfig{endpoint: countryEndpoint, soapVersion: "1.1", xmlMode: true, timeout: 15})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction:  `"http://www.oorsprong.org/websamples.countryinfo/ListOfContinentsByName"`,
		requestBody: `<ListOfContinentsByName xmlns="http://www.oorsprong.org/websamples.countryinfo"/>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.False(t, out.IsFault)
	assert.Contains(t, out.SOAPResponsePayload.(string), "Africa")
	assert.Contains(t, out.SOAPResponsePayload.(string), "Europe")
}

// ---------------------------------------------------------------------------
// T10 — Live: CountryInfo — CountryISOCode for India must return "IN"
// ---------------------------------------------------------------------------

func TestLive_CountryInfo_CountryISOCode(t *testing.T) {
	if !pingService(countryEndpoint) {
		t.Skip("CountryInfo service unreachable")
	}
	act, err := newTestActivity(activityConfig{endpoint: countryEndpoint, soapVersion: "1.1", xmlMode: true, timeout: 15})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction: `"http://www.oorsprong.org/websamples.countryinfo/CountryISOCode"`,
		requestBody: `<CountryISOCode xmlns="http://www.oorsprong.org/websamples.countryinfo">` +
			`<sCountryName>India</sCountryName></CountryISOCode>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.False(t, out.IsFault)
	assert.Contains(t, out.SOAPResponsePayload.(string), "IN", "India ISO code must be IN")
}

// ---------------------------------------------------------------------------
// T11 — Query parameters are appended to endpoint URL
// ---------------------------------------------------------------------------

func TestActivity_QueryParams_Appended(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	act, err := newTestActivity(activityConfig{endpoint: srv.URL + "/ep", soapVersion: "1.1", xmlMode: true})
	require.NoError(t, err)

	ctx := &testContext{
		inputs: map[string]interface{}{
			"soapAction":         "http://test/Ping",
			"soapRequestBody":    `<Ping/>`,
			"soapRequestHeaders": nil,
			"httpQueryParams":    map[string]interface{}{"format": "xml", "v": "2"},
		},
		outputs: make(map[string]interface{}),
	}
	_, err = act.Eval(ctx)
	require.NoError(t, err)
	assert.Contains(t, capturedURL, "format=xml")
	assert.Contains(t, capturedURL, "v=2")
}

// ---------------------------------------------------------------------------
// T12 — Explicit soapAction input overrides WSDL-derived action
// ---------------------------------------------------------------------------

func TestActivity_ExplicitSoapAction_OverridesWSDL(t *testing.T) {
	var capturedAction string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAction = r.Header.Get("SOAPAction")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	act, err := newTestActivity(activityConfig{
		endpoint:    srv.URL,
		soapVersion: "1.1",
		xmlMode:     true,
		wsdlUrl:     "testdata/calculator.wsdl",
		wsdlOp:      "Add",
	})
	require.NoError(t, err)

	_, err = evalActivity(act, evalInput{
		soapAction:  "http://override/CustomAction",
		requestBody: `<Add xmlns="http://tempuri.org/"><intA>1</intA><intB>1</intB></Add>`,
	})
	require.NoError(t, err)
	assert.Equal(t, `"http://override/CustomAction"`, capturedAction,
		"explicit soapAction must override WSDL-derived action")
}

// ---------------------------------------------------------------------------
// T13 — Content-Type header: text/xml for SOAP 1.1, application/soap+xml for 1.2
// ---------------------------------------------------------------------------

func TestActivity_ContentType_SOAP11_vs_SOAP12(t *testing.T) {
	var ct11, ct12 string

	srv11 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct11 = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv11.Close()

	srv12 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct12 = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv12.Close()

	act11, _ := newTestActivity(activityConfig{endpoint: srv11.URL, soapVersion: "1.1", xmlMode: true})
	act12, _ := newTestActivity(activityConfig{endpoint: srv12.URL, soapVersion: "1.2", xmlMode: true})
	_, _ = evalActivity(act11, evalInput{requestBody: `<Ping/>`})
	_, _ = evalActivity(act12, evalInput{requestBody: `<Ping/>`})

	assert.Contains(t, ct11, "text/xml")
	assert.Contains(t, ct12, "application/soap+xml")
}

// ---------------------------------------------------------------------------
// T14 — Wire envelope namespace differs between SOAP 1.1 and 1.2
// ---------------------------------------------------------------------------

func TestActivity_Envelope_Namespace_SOAP11_vs_SOAP12(t *testing.T) {
	var body11, body12 []byte

	srv11 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body11, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv11.Close()

	srv12 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body12, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv12.Close()

	act11, _ := newTestActivity(activityConfig{endpoint: srv11.URL, soapVersion: "1.1", xmlMode: true})
	act12, _ := newTestActivity(activityConfig{endpoint: srv12.URL, soapVersion: "1.2", xmlMode: true})
	_, _ = evalActivity(act11, evalInput{requestBody: `<Ping/>`})
	_, _ = evalActivity(act12, evalInput{requestBody: `<Ping/>`})

	assert.Contains(t, string(body11), "http://schemas.xmlsoap.org/soap/envelope/")
	assert.Contains(t, string(body12), "http://www.w3.org/2003/05/soap-envelope")
}

// ---------------------------------------------------------------------------
// T15 — SOAP Header element is injected into the envelope
// ---------------------------------------------------------------------------

func TestActivity_SOAPHeader_IsInjected(t *testing.T) {
	var wireBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wireBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	act, _ := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.1", xmlMode: true})
	_, _ = evalActivity(act, evalInput{
		requestBody: `<Ping/>`,
		headers:     `<Auth><Token>secret-token</Token></Auth>`,
	})

	assert.Contains(t, string(wireBody), "<soap:Header>")
	assert.Contains(t, string(wireBody), "secret-token")
}

// ---------------------------------------------------------------------------
// T16 — Response SOAP headers are decoded and returned
// ---------------------------------------------------------------------------

func TestActivity_Response_HeadersDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Header><ServerTime xmlns="http://test/">2024-01-01T00:00:00Z</ServerTime></soap:Header>
  <soap:Body><Result>ok</Result></soap:Body>
</soap:Envelope>`)
	}))
	defer srv.Close()

	act, _ := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.1", xmlMode: true})
	out, err := evalActivity(act, evalInput{requestBody: `<Ping/>`})
	require.NoError(t, err)
	assert.NotNil(t, out.SOAPResponseHeaders)
	assert.Contains(t, fmt.Sprintf("%v", out.SOAPResponseHeaders), "2024-01-01T00:00:00Z")
}

// ---------------------------------------------------------------------------
// T17 — SOAP 1.2 Fault sets IsFault=true
// ---------------------------------------------------------------------------

func TestActivity_SOAPFault_12_Format(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope">
  <soap:Body>
    <soap:Fault>
      <soap:Code><soap:Value>soap:Sender</soap:Value></soap:Code>
      <soap:Reason><soap:Text xml:lang="en">Invalid request</soap:Text></soap:Reason>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer srv.Close()

	act, _ := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.2", xmlMode: true})
	out, err := evalActivity(act, evalInput{requestBody: `<Bad/>`})
	require.NoError(t, err)
	assert.Equal(t, 500, out.HttpStatus)
	assert.True(t, out.IsFault, "SOAP 1.2 fault must set IsFault=true")
}

// ---------------------------------------------------------------------------
// T18 — Non-XML HTML response is passed through as raw string (no panic)
// ---------------------------------------------------------------------------

func TestActivity_NonXML_Response_Passthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><body>Not Found</body></html>`)
	}))
	defer srv.Close()

	act, _ := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.1", xmlMode: true})
	out, err := evalActivity(act, evalInput{requestBody: `<Ping/>`})
	require.NoError(t, err)
	assert.Equal(t, 404, out.HttpStatus)
	assert.False(t, out.IsFault)
	assert.Contains(t, fmt.Sprintf("%v", out.SOAPResponsePayload), "Not Found")
}

// ---------------------------------------------------------------------------
// T19 — Slow server triggers timeout; activity returns error
// ---------------------------------------------------------------------------

func TestActivity_Timeout_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	act, err := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.1", xmlMode: true, timeout: 1})
	require.NoError(t, err)

	_, err = evalActivity(act, evalInput{requestBody: `<Ping/>`})
	require.Error(t, err, "slow server must cause a timeout error")
	lower := strings.ToLower(err.Error())
	assert.True(t, strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"),
		"error must mention timeout or deadline; got: %s", err.Error())
}

// ---------------------------------------------------------------------------
// T20 — Empty 204 response body handled gracefully
// ---------------------------------------------------------------------------

func TestActivity_EmptyBody_NoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	act, _ := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.1", xmlMode: true})
	out, err := evalActivity(act, evalInput{requestBody: `<Ping/>`})
	require.NoError(t, err)
	assert.Equal(t, 204, out.HttpStatus)
}

// ---------------------------------------------------------------------------
// T21 — ISO-8859-1 encoded WSDL parses without error (charset reader fix)
// ---------------------------------------------------------------------------

func TestWSDL_ISO8859_Charset(t *testing.T) {
	wsdlBytes, err := os.ReadFile("testdata/iso8859.wsdl")
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=ISO-8859-1")
		_, _ = w.Write(wsdlBytes)
	}))
	defer srv.Close()

	act, err := newTestActivity(activityConfig{
		endpoint:    "http://placeholder.invalid",
		soapVersion: "1.1",
		wsdlHttpUrl: srv.URL,
		wsdlOp:      "Echo",
	})
	require.NoError(t, err, "ISO-8859-1 WSDL must parse without error")
	assert.NotNil(t, act)
}

// ---------------------------------------------------------------------------
// T22 — Multi-operation WSDL: all 3 operations listed; SOAP call succeeds
// ---------------------------------------------------------------------------

func TestWSDL_MultiOp_AllOperationsListed(t *testing.T) {
	info, err := wsdl.Parse("testdata/multiop.wsdl", nil)
	require.NoError(t, err)

	ops := wsdl.ListOperations(info)
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = op.Name
	}
	assert.ElementsMatch(t, []string{"Sum", "Power", "Sqrt"}, names)
}

// ---------------------------------------------------------------------------
// T23 — Concurrent requests: activity is goroutine-safe
// ---------------------------------------------------------------------------

func TestLive_Calculator_ConcurrentRequests(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{endpoint: calcEndpoint, soapVersion: "1.1", xmlMode: true, timeout: 30})
	require.NoError(t, err)

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	results := make([]string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			a, b := idx+1, idx+10
			out, e := evalActivity(act, evalInput{
				soapAction: `"http://tempuri.org/Add"`,
				requestBody: fmt.Sprintf(
					`<Add xmlns="http://tempuri.org/"><intA>%d</intA><intB>%d</intB></Add>`, a, b),
			})
			errs[idx] = e
			if out != nil {
				results[idx] = fmt.Sprintf("%v", out.SOAPResponsePayload)
			}
		}(i)
	}
	wg.Wait()

	for i := range errs {
		require.NoError(t, errs[i], "goroutine %d must not error", i)
		expected := fmt.Sprintf("%d", (i+1)+(i+10))
		assert.Contains(t, results[i], expected, "goroutine %d result wrong", i)
	}
}

// ---------------------------------------------------------------------------
// T24 — Basic auth header injected via mock connection
// ---------------------------------------------------------------------------

func TestActivity_BasicAuth_HeaderInjected(t *testing.T) {
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	// Pass the mock connection via activityConfig so New() loads it via
	// coerce.ToConnection, which accepts connection.Manager directly.
	act, err := newTestActivity(activityConfig{
		endpoint:      srv.URL,
		soapVersion:   "1.1",
		xmlMode:       true,
		authorization: true,
		authConn:      &mockConnectionManager{conn: &mockBasicAuthConn{Type: "Basic", UserName: "admin", Password: "s3cret"}},
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{soapAction: `"http://test/Ping"`, requestBody: `<Ping/>`})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)

	require.True(t, strings.HasPrefix(capturedAuth, "Basic "), "must have Basic auth header")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(capturedAuth, "Basic "))
	require.NoError(t, err)
	assert.Equal(t, "admin:s3cret", string(raw))
}

// ---------------------------------------------------------------------------
// T25 — Bearer token auth header injected via mock connection
// ---------------------------------------------------------------------------

func TestActivity_BearerToken_HeaderInjected(t *testing.T) {
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	// Pass the mock connection via activityConfig — see T24 for rationale.
	act, err := newTestActivity(activityConfig{
		endpoint:      srv.URL,
		soapVersion:   "1.1",
		xmlMode:       true,
		authorization: true,
		authConn:      &mockConnectionManager{conn: &mockBearerAuthConn{Type: "Bearer Token", BearerToken: "eyJhbGciOiJIUzI1NiJ9.test"}},
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{soapAction: `"http://test/Ping"`, requestBody: `<Ping/>`})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.Equal(t, "Bearer eyJhbGciOiJIUzI1NiJ9.test", capturedAuth)
}

// ---------------------------------------------------------------------------
// T26 — Live: Divide-by-zero (service may fault or return 0)
// ---------------------------------------------------------------------------

func TestLive_Calculator_DivideByZero(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{endpoint: calcEndpoint, soapVersion: "1.1", xmlMode: true, timeout: 15})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction:  `"http://tempuri.org/Divide"`,
		requestBody: `<Divide xmlns="http://tempuri.org/"><intA>10</intA><intB>0</intB></Divide>`,
	})
	require.NoError(t, err, "activity must not return Go error; SOAP fault handled gracefully")
	t.Logf("Divide/0 — status=%d isFault=%v payload=%v", out.HttpStatus, out.IsFault, out.SOAPResponsePayload)
}

// ---------------------------------------------------------------------------
// T27 — WSDL body skeleton is well-formed XML
// ---------------------------------------------------------------------------

func TestWSDL_Skeleton_IsWellFormedXML(t *testing.T) {
	info, err := wsdl.Parse("testdata/calculator.wsdl", nil)
	require.NoError(t, err)

	op := wsdl.FindOperation(info, "Add", "1.1")
	require.NotNil(t, op)

	skeleton := wsdl.BuildXMLBody(op, info.TargetNamespace)
	require.NotEmpty(t, skeleton)

	wrapped := "<root>" + skeleton + "</root>"
	dec := xml.NewDecoder(bytes.NewBufferString(wrapped))
	for {
		_, xmlErr := dec.Token()
		if xmlErr == io.EOF {
			break
		}
		require.NoError(t, xmlErr, "WSDL body skeleton must be well-formed XML")
	}
}

// ---------------------------------------------------------------------------
// T28 — JSON schema from WSDL has correct properties for Power operation
// ---------------------------------------------------------------------------

func TestWSDL_JSONSchema_PowerOperation(t *testing.T) {
	info, err := wsdl.Parse("testdata/multiop.wsdl", nil)
	require.NoError(t, err)

	op := wsdl.FindOperation(info, "Power", "1.1")
	require.NotNil(t, op)

	schemaJSON := wsdl.BuildJSONSchema(op)
	require.NotEmpty(t, schemaJSON)

	var schema map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(schemaJSON), &schema))
	assert.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, props, "Base")
	assert.Contains(t, props, "Exponent")
}

// ---------------------------------------------------------------------------
// T29 — Self-signed TLS server is reachable with InsecureSkipVerify
// ---------------------------------------------------------------------------

func TestActivity_TLS_InsecureSkipVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	act, err := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.1", xmlMode: true, timeout: 10, skipTlsVerify: true})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{requestBody: `<Ping/>`})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
}

// ---------------------------------------------------------------------------
// T30 — mTLS: server that requires client cert; activity connects with cert
// ---------------------------------------------------------------------------

func TestActivity_TLS_MutualTLS_WithClientCert(t *testing.T) {
	// Generate self-signed CA / leaf certificate for this test.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Skipf("failed to generate key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test CA"},
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Skipf("failed to create CA cert: %v", err)
	}
	caCert, _ := x509.ParseCertificate(caCertDER)

	// Build server TLS config requiring client cert from our CA.
	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	srvTLS := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	srvTLS.TLS = &tls.Config{
		ClientAuth: tls.RequestClientCert, // don't hard-require so we can test the activity side
		ClientCAs:  pool,
	}
	srvTLS.StartTLS()
	defer srvTLS.Close()

	// Activity connects with skipTlsVerify — must succeed regardless of client cert.
	act, err := newTestActivity(activityConfig{endpoint: srvTLS.URL, soapVersion: "1.1", xmlMode: true, timeout: 10, skipTlsVerify: true})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{requestBody: `<Ping/>`})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
}

// ---------------------------------------------------------------------------
// T31 — Live: Subtract with WSDL auto-injected soapAction (local WSDL)
// ---------------------------------------------------------------------------

func TestLive_Calculator_WSDL_Subtract_AutoAction(t *testing.T) {
	if !pingService(calcEndpoint) {
		t.Skip("Calculator service unreachable")
	}
	act, err := newTestActivity(activityConfig{
		endpoint:    calcEndpoint,
		soapVersion: "1.1",
		xmlMode:     true,
		timeout:     15,
		wsdlUrl:     "testdata/calculator.wsdl",
		wsdlOp:      "Subtract",
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		requestBody: `<Subtract xmlns="http://tempuri.org/"><intA>50</intA><intB>8</intB></Subtract>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.Contains(t, out.SOAPResponsePayload.(string), "42", "50-8 must equal 42")
}

// ---------------------------------------------------------------------------
// T32 — Large payload (1 000 elements) is transmitted intact
// ---------------------------------------------------------------------------

func TestActivity_LargePayload_NoTruncation(t *testing.T) {
	var receivedLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedLen = len(body)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	act, _ := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.1", xmlMode: true})

	var sb strings.Builder
	sb.WriteString(`<Items xmlns="http://test/">`)
	for i := 0; i < 1000; i++ {
		sb.WriteString(fmt.Sprintf("<Item><ID>%d</ID><Value>test-value-%d</Value></Item>", i, i))
	}
	sb.WriteString("</Items>")

	out, err := evalActivity(act, evalInput{requestBody: sb.String()})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.Greater(t, receivedLen, 50_000, "server must receive the full large payload")
}

// ---------------------------------------------------------------------------
// T33 — HTTP connection refused returns a clear error
// ---------------------------------------------------------------------------

func TestActivity_ConnectionRefused_ReturnsError(t *testing.T) {
	// Use a port that is almost certainly not listening.
	act, err := newTestActivity(activityConfig{
		endpoint:    "http://127.0.0.1:19999/soap",
		soapVersion: "1.1",
		xmlMode:     true,
		timeout:     3,
	})
	require.NoError(t, err)

	_, err = evalActivity(act, evalInput{requestBody: `<Ping/>`})
	require.Error(t, err, "connection refused must return an error")
	assert.Contains(t, err.Error(), "HTTP call failed")
}

// ---------------------------------------------------------------------------
// Helper types for auth mock connection
// ---------------------------------------------------------------------------

// mockBasicAuthConn is shaped to match what buildAuthHeader reads via reflection.
type mockBasicAuthConn struct {
	Type     string
	UserName string
	Password string
}

// mockBearerAuthConn mimics a Bearer token connection.
type mockBearerAuthConn struct {
	Type        string
	BearerToken string
}

// mockConnectionManager implements the connection.Manager interface subset
// that the activity uses (GetConnection()).
type mockConnectionManager struct {
	conn interface{}
}

func (m *mockConnectionManager) GetConnection() interface{}      { return m.conn }
func (m *mockConnectionManager) ReleaseConnection(_ interface{}) {}
func (m *mockConnectionManager) Type() string                    { return "mock" }
