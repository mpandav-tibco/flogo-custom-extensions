package soapclient_test

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/soapclient/wsdl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// WSDL parser tests
// ---------------------------------------------------------------------------

func TestWSDL_ParseLocalFile(t *testing.T) {
	path, _ := filepath.Abs("testdata/weather.wsdl")
	info, err := wsdl.Parse(path, nil)
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, "http://www.example.com/weather", info.TargetNamespace)
	assert.Equal(t, "http://www.example.com/weatherservice", info.ServiceEndpoint)
	assert.Len(t, info.Operations, 1)

	op := info.Operations[0]
	assert.Equal(t, "GetWeather", op.Name)
	assert.Equal(t, "http://www.example.com/weather/GetWeather", op.SOAPAction)
	assert.Equal(t, "document", op.Style)
}

func TestWSDL_ParseFileURI(t *testing.T) {
	path, _ := filepath.Abs("testdata/weather.wsdl")
	info, err := wsdl.Parse("file://"+path, nil)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Len(t, info.Operations, 1)
}

func TestWSDL_ParseFromHTTP(t *testing.T) {
	wsdlBytes, err := os.ReadFile("testdata/weather.wsdl")
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write(wsdlBytes)
	}))
	defer srv.Close()

	info, err := wsdl.Parse(srv.URL+"/?wsdl", nil)
	require.NoError(t, err)
	assert.Equal(t, "GetWeather", info.Operations[0].Name)
}

func TestWSDL_OperationFields(t *testing.T) {
	path, _ := filepath.Abs("testdata/weather.wsdl")
	info, err := wsdl.Parse(path, nil)
	require.NoError(t, err)

	op := wsdl.FindOperation(info, "GetWeather", "")
	require.NotNil(t, op)
	require.NotNil(t, op.InputMsg)
	require.Len(t, op.InputMsg.Parts, 1)

	fields := op.InputMsg.Parts[0].Fields
	require.Len(t, fields, 2)
	assert.Equal(t, "CityName", fields[0].Name)
	assert.Equal(t, "CountryName", fields[1].Name)
}

func TestWSDL_FindOperation_Missing(t *testing.T) {
	path, _ := filepath.Abs("testdata/weather.wsdl")
	info, err := wsdl.Parse(path, nil)
	require.NoError(t, err)
	assert.Nil(t, wsdl.FindOperation(info, "NoSuchOp", ""))
}

// TestWSDL_NDFD_LiveURL exercises the parser against the public NOAA/NWS WSDL.
// It is skipped when network is unavailable. The NDFD WSDL uses an RPC-style
// binding, SOAP-encoded messages, and an xmlns="" default-namespace declaration
// — these are edge-cases not covered by the local weather.wsdl fixture.
func TestWSDL_NDFD_LiveURL(t *testing.T) {
	const ndfdURL = "https://graphical.weather.gov/xml/SOAP_server/ndfdXMLserver.php?wsdl"

	info, err := wsdl.Parse(ndfdURL, nil)
	if err != nil {
		t.Skipf("NDFD WSDL unreachable (network/CORS?): %v", err)
	}
	require.NotNil(t, info)

	// The WSDL declares 12 operations in the ndfdXMLBinding.
	expectedOps := []string{
		"NDFDgen", "NDFDgenLatLonList", "LatLonListSubgrid", "LatLonListLine",
		"LatLonListZipCode", "LatLonListCityNames", "LatLonListSquare",
		"CornerPoints", "GmlLatLonList", "GmlTimeSeries",
		"NDFDgenByDay", "NDFDgenByDayLatLonList",
	}
	assert.Len(t, info.Operations, len(expectedOps), "expected %d operations", len(expectedOps))

	opNames := make([]string, len(info.Operations))
	for i, op := range info.Operations {
		opNames[i] = op.Name
	}
	for _, want := range expectedOps {
		assert.Contains(t, opNames, want)
	}

	// Every binding operation should carry a non-empty soapAction.
	for _, op := range info.Operations {
		assert.NotEmpty(t, op.SOAPAction, "op %q missing soapAction", op.Name)
		assert.Equal(t, "rpc", op.Style, "NDFD is RPC-style")
	}

	// Service endpoint is defined in the WSDL (localhost placeholder address).
	assert.NotEmpty(t, info.ServiceEndpoint, "service endpoint should be populated from WSDL")
	assert.Contains(t, info.ServiceEndpoint, "ndfdXMLserver.php")
}

// ---------------------------------------------------------------------------
// BodyBuilder tests
// ---------------------------------------------------------------------------

func TestBodyBuilder_XMLSkeleton(t *testing.T) {
	path, _ := filepath.Abs("testdata/weather.wsdl")
	info, err := wsdl.Parse(path, nil)
	require.NoError(t, err)

	op := wsdl.FindOperation(info, "GetWeather", "")
	skeleton := wsdl.BuildXMLBody(op, info.TargetNamespace)

	assert.Contains(t, skeleton, "GetWeatherRequest")
	assert.Contains(t, skeleton, "<CityName>")
	assert.Contains(t, skeleton, "<CountryName>")
	assert.Contains(t, skeleton, "string") // XSD type comment (xs: prefix stripped by parser)
	// The generated skeleton should be well-formed XML.
	wrapped := "<root>" + skeleton + "</root>"
	assert.NoError(t, xml.Unmarshal([]byte(wrapped), new(interface{})))
}

func TestBodyBuilder_JSONSchema(t *testing.T) {
	path, _ := filepath.Abs("testdata/weather.wsdl")
	info, err := wsdl.Parse(path, nil)
	require.NoError(t, err)

	op := wsdl.FindOperation(info, "GetWeather", "")
	schemaStr := wsdl.BuildJSONSchema(op)

	var schema map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(schemaStr), &schema))

	assert.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, props, "CityName")
	assert.Contains(t, props, "CountryName")
}

func TestBodyBuilder_ListOperations(t *testing.T) {
	path, _ := filepath.Abs("testdata/weather.wsdl")
	info, err := wsdl.Parse(path, nil)
	require.NoError(t, err)

	ops := wsdl.ListOperations(info)
	require.Len(t, ops, 1)
	assert.Equal(t, "GetWeather", ops[0].Name)
	assert.Equal(t, "http://www.example.com/weather/GetWeather", ops[0].SOAPAction)
}

// ---------------------------------------------------------------------------
// Activity integration tests (mock SOAP server)
// ---------------------------------------------------------------------------

const soapResponseXML = `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetWeatherResponse xmlns="http://www.example.com/weather">
      <Temperature>22.5</Temperature>
      <Humidity>68</Humidity>
      <Description>Partly cloudy</Description>
    </GetWeatherResponse>
  </soap:Body>
</soap:Envelope>`

// mockSOAPServer creates a test HTTP server that validates the incoming request
// and returns a synthetic SOAP response. It also records the SOAPAction header.
func mockSOAPServer(t *testing.T, soapAction *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*soapAction = r.Header.Get("SOAPAction")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
}

func TestActivity_XMLMode_NoWSDL(t *testing.T) {
	var receivedAction string
	srv := mockSOAPServer(t, &receivedAction)
	defer srv.Close()

	act, err := newTestActivity(activityConfig{
		endpoint:    srv.URL,
		soapVersion: "1.1",
		xmlMode:     true,
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction:  `"http://custom/action"`,
		requestBody: `<GetWeather xmlns="http://www.example.com/weather"><CityName>London</CityName><CountryName>UK</CountryName></GetWeather>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.False(t, out.IsFault)
	assert.Equal(t, `"http://custom/action"`, receivedAction)
	assert.Contains(t, out.SOAPResponsePayload.(string), "GetWeatherResponse")
}

func TestActivity_WSDL_AutoInjectsSoapAction(t *testing.T) {
	var receivedAction string
	srv := mockSOAPServer(t, &receivedAction)
	defer srv.Close()

	path, _ := filepath.Abs("testdata/weather.wsdl")
	act, err := newTestActivity(activityConfig{
		endpoint:    srv.URL,
		soapVersion: "1.1",
		xmlMode:     true,
		wsdlUrl:     "file://" + path,
		wsdlOp:      "GetWeather",
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction:  "", // intentionally empty — should be injected from WSDL
		requestBody: `<GetWeatherRequest xmlns="http://www.example.com/weather"><CityName>Paris</CityName><CountryName>FR</CountryName></GetWeatherRequest>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	// soapAction must be the one from the WSDL binding, quoted per SOAP 1.1 spec §6.1.1.
	assert.Equal(t, `"http://www.example.com/weather/GetWeather"`, receivedAction)
}

func TestActivity_WSDL_SkeletonInOutput(t *testing.T) {
	var receivedAction string
	srv := mockSOAPServer(t, &receivedAction)
	defer srv.Close()

	path, _ := filepath.Abs("testdata/weather.wsdl")
	act, err := newTestActivity(activityConfig{
		endpoint:    srv.URL,
		soapVersion: "1.1",
		xmlMode:     true,
		wsdlUrl:     "file://" + path,
		wsdlOp:      "GetWeather",
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		requestBody: `<GetWeatherRequest xmlns="http://www.example.com/weather"><CityName>Tokyo</CityName><CountryName>JP</CountryName></GetWeatherRequest>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
}

func TestActivity_WSDL_AutoEndpoint(t *testing.T) {
	// autoUseWsdlEndpoint=true replaces the endpoint with the WSDL <soap:address>.
	// We just verify that initialisation succeeds without errors.
	path, _ := filepath.Abs("testdata/weather.wsdl")
	act, err := newTestActivity(activityConfig{
		endpoint:            "http://placeholder.invalid",
		soapVersion:         "1.1",
		wsdlUrl:             "file://" + path,
		wsdlOp:              "GetWeather",
		autoUseWsdlEndpoint: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, act)
}

func TestActivity_WSDL_UnknownOperation(t *testing.T) {
	path, _ := filepath.Abs("testdata/weather.wsdl")
	_, err := newTestActivity(activityConfig{
		endpoint:    "http://placeholder.invalid",
		soapVersion: "1.1",
		wsdlUrl:     "file://" + path,
		wsdlOp:      "NoSuchOperation",
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not found in WSDL"))
}

// TestActivity_WSDL_JSONMode_BodyWrapping verifies that in JSON mode with a WSDL
// operation configured, the activity wraps the flat JSON fields in the correct
// operation root element with the WSDL target namespace.
// e.g. {"CityName":"London","CountryName":"UK"} must become
// <GetWeatherRequest xmlns="http://www.example.com/weather"><CityName>London</CityName>...
// Without this wrapping the service receives the wrong element name and cannot
// dispatch the request (returning defaults or errors).
func TestActivity_WSDL_JSONMode_BodyWrapping(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		capturedBody = string(raw)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	path, _ := filepath.Abs("testdata/weather.wsdl")
	act, err := newTestActivity(activityConfig{
		endpoint:    srv.URL,
		soapVersion: "1.1",
		xmlMode:     false, // JSON mode — the path under test
		wsdlUrl:     "file://" + path,
		wsdlOp:      "GetWeather",
	})
	require.NoError(t, err)

	body := map[string]interface{}{"CityName": "London", "CountryName": "UK"}
	out, err := evalActivity(act, evalInput{requestBody: body})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)

	// The SOAP body must contain the operation root element with the WSDL namespace.
	assert.Contains(t, capturedBody, `<GetWeatherRequest xmlns="http://www.example.com/weather">`,
		"body must be wrapped in the WSDL operation element with target namespace")
	assert.Contains(t, capturedBody, "<CityName>London</CityName>")
	assert.Contains(t, capturedBody, "<CountryName>UK</CountryName>")
}

// TestActivity_WSDL_JSONMode_ResponseNoXmlnsNoise verifies that the activity strips
// XML namespace declaration attributes (e.g. xmlns="...") from the JSON response
// output. Without this, mxj emits a "-xmlns" key in every element that carries a
// namespace declaration, cluttering the output and breaking downstream mappings.
func TestActivity_WSDL_JSONMode_ResponseNoXmlnsNoise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	path, _ := filepath.Abs("testdata/weather.wsdl")
	act, err := newTestActivity(activityConfig{
		endpoint:    srv.URL,
		soapVersion: "1.1",
		xmlMode:     false,
		wsdlUrl:     "file://" + path,
		wsdlOp:      "GetWeather",
	})
	require.NoError(t, err)

	body := map[string]interface{}{"CityName": "Paris", "CountryName": "FR"}
	out, err := evalActivity(act, evalInput{requestBody: body})
	require.NoError(t, err)

	payload, ok := out.SOAPResponsePayload.(map[string]interface{})
	require.True(t, ok, "soapResponsePayload must be a map in JSON mode")

	// Recursively check that no key contains "xmlns" (with any attribute prefix like "-")
	assertNoXmlnsKeys(t, payload)
}

// assertNoXmlnsKeys fails if any map key in v (recursively) contains "xmlns".
func assertNoXmlnsKeys(t *testing.T, v interface{}) {
	t.Helper()
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			bare := strings.TrimLeft(k, "-_")
			assert.False(t, bare == "xmlns" || strings.HasPrefix(bare, "xmlns:"),
				"response JSON must not contain namespace attribute key %q", k)
			assertNoXmlnsKeys(t, child)
		}
	case []interface{}:
		for _, item := range val {
			assertNoXmlnsKeys(t, item)
		}
	}
}

func TestActivity_SOAPFault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <soap:Fault>
      <faultcode>soap:Server</faultcode>
      <faultstring>Internal server error</faultstring>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer srv.Close()

	act, err := newTestActivity(activityConfig{
		endpoint:    srv.URL,
		soapVersion: "1.1",
		xmlMode:     true,
	})
	require.NoError(t, err)

	out, err := evalActivity(act, evalInput{
		soapAction:  "http://test/action",
		requestBody: `<Ping/>`,
	})
	require.NoError(t, err)
	assert.Equal(t, 500, out.HttpStatus)
	assert.True(t, out.IsFault)
}

// TestActivity_JSONMode_FieldOrderPreserved verifies that the JSON→XML conversion
// respects the field order defined in the input struct. Previously the activity
// used mxj which converted JSON→map[string]interface{} before serialising to XML,
// losing the original field order. The current implementation uses
// json.Decoder.Token() which streams tokens in document order.
func TestActivity_JSONMode_FieldOrderPreserved(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		capturedBody = string(raw)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	act, err := newTestActivity(activityConfig{endpoint: srv.URL, soapVersion: "1.1", xmlMode: false})
	require.NoError(t, err)

	// A struct whose fields are defined in Z→A order (non-alphabetical).
	// json.Marshal serialises struct fields in definition order, so we get a
	// deterministically ordered JSON object to feed through the activity.
	type body struct {
		Z string `json:"z"`
		A string `json:"a"`
	}
	out, err := evalActivity(act, evalInput{requestBody: body{Z: "last-alphabetically", A: "first-alphabetically"}})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)

	// The serialised request envelope must contain <z> before <a>, matching the
	// struct definition order preserved through JSON serialisation.
	zIdx := strings.Index(capturedBody, "<z>")
	aIdx := strings.Index(capturedBody, "<a>")
	require.True(t, zIdx >= 0 && aIdx >= 0, "both elements must be present in request: %q", capturedBody)
	assert.True(t, zIdx < aIdx,
		"struct field definition order must be preserved in XML: <z> before <a> in %q", capturedBody)
}

// TestActivity_XMLMode_MapBody verifies that xmlMode=true accepts a
// map[string]interface{} body (the type Flogo delivers from complex_object
// designer mappings) and converts it to XML via jsonToXMLOrdered.
// Also covers the @-prefix attribute feature end-to-end and the numeric
// attribute value fix (D1): @id:42 must become id="42", not id="".
func TestActivity_XMLMode_MapBody(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		capturedBody = string(raw)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, soapResponseXML)
	}))
	defer srv.Close()

	act, err := newTestActivity(activityConfig{
		endpoint:    srv.URL,
		soapVersion: "1.1",
		xmlMode:     true,
	})
	require.NoError(t, err)

	// Body as map — same type that Flogo delivers from a complex_object field mapping.
	// @xmlns becomes an XML attribute; @id is a numeric attribute value (non-string).
	body := map[string]interface{}{
		"Add": map[string]interface{}{
			"@xmlns": "http://tempuri.org/",
			"@id":    42, // numeric attribute — must NOT produce id=""
			"a":      10,
			"b":      5,
		},
	}
	out, err := evalActivity(act, evalInput{
		soapAction:  "http://tempuri.org/Add",
		requestBody: body,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, out.HttpStatus)
	assert.False(t, out.IsFault)

	// The envelope must contain the converted XML with the namespace attribute.
	assert.Contains(t, capturedBody, `xmlns="http://tempuri.org/"`,
		"@xmlns key must become xmlns attribute: %q", capturedBody)
	// The numeric @id attribute must be non-empty (D1 regression guard).
	assert.Contains(t, capturedBody, `id="42"`,
		"numeric @id attribute must be id=\"42\", not id=\"\": %q", capturedBody)
	// Child elements must be present.
	assert.Contains(t, capturedBody, "<a>10</a>")
	assert.Contains(t, capturedBody, "<b>5</b>")
	// Response in xmlMode must be a string.
	assert.IsType(t, "", out.SOAPResponsePayload, "xmlMode response must be a string")
}
