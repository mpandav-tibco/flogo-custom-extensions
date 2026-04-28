// Package soapclient — internal tests for the jsonToXMLOrdered converter.
// These tests live in package soapclient (not soapclient_test) so they can
// access unexported functions directly.
package soapclient

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSONToXMLOrdered_FlatObject verifies that a flat JSON object produces
// elements in the exact order the keys appear in the JSON bytes.
func TestJSONToXMLOrdered_FlatObject(t *testing.T) {
	// Keys are deliberately in non-alphabetical order: c, a, b.
	got, err := jsonToXMLOrdered([]byte(`{"c":3,"a":1,"b":2}`))
	require.NoError(t, err)
	result := string(got)

	cIdx := strings.Index(result, "<c>")
	aIdx := strings.Index(result, "<a>")
	bIdx := strings.Index(result, "<b>")
	require.True(t, cIdx >= 0 && aIdx >= 0 && bIdx >= 0, "all elements present: %q", result)
	assert.True(t, cIdx < aIdx && aIdx < bIdx,
		"elements must appear in JSON key order (c→a→b), got: %q", result)
}

// TestJSONToXMLOrdered_NestedObject verifies that nested object key order is
// also preserved.
func TestJSONToXMLOrdered_NestedObject(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"Add":{"intB":2,"intA":1}}`))
	require.NoError(t, err)
	result := string(got)

	assert.Equal(t, `<Add><intB>2</intB><intA>1</intA></Add>`, result)
}

// TestJSONToXMLOrdered_Array verifies that JSON arrays produce one element
// per item, each wrapped with the parent key name.
func TestJSONToXMLOrdered_Array(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"item":[1,2,3]}`))
	require.NoError(t, err)
	assert.Equal(t, `<item>1</item><item>2</item><item>3</item>`, string(got))
}

// TestJSONToXMLOrdered_ArrayOfObjects verifies nested objects inside arrays.
func TestJSONToXMLOrdered_ArrayOfObjects(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"row":[{"z":"last","a":"first"},{"z":"2nd","a":"also"}]}`))
	require.NoError(t, err)
	result := string(got)
	// First row: <z> must appear before <a>.
	firstZ := strings.Index(result, "<z>")
	firstA := strings.Index(result, "<a>")
	assert.True(t, firstZ < firstA, "key order preserved in first array item: %q", result)
}

// TestJSONToXMLOrdered_XMLEscaping verifies that special XML characters in
// string values are properly escaped.
func TestJSONToXMLOrdered_XMLEscaping(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"msg":"<hello> & world"}`))
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, "&lt;hello&gt;")
	assert.Contains(t, result, "&amp;")
	assert.NotContains(t, result, "<hello>", "raw < must be escaped")
}

// TestJSONToXMLOrdered_NullValue verifies that a JSON null produces an empty
// element rather than an error.
func TestJSONToXMLOrdered_NullValue(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"empty":null}`))
	require.NoError(t, err)
	assert.Equal(t, `<empty></empty>`, string(got))
}

// TestJSONToXMLOrdered_BoolAndNumber verifies numeric and boolean scalar types.
func TestJSONToXMLOrdered_BoolAndNumber(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"flag":true,"count":42,"ratio":3.14}`))
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, "<flag>true</flag>")
	assert.Contains(t, result, "<count>42</count>")
	assert.Contains(t, result, "<ratio>3.14</ratio>")
}

// TestJSONToXMLOrdered_ErrorOnArray verifies that a top-level JSON array is
// rejected (SOAP bodies must be objects).
func TestJSONToXMLOrdered_ErrorOnArray(t *testing.T) {
	_, err := jsonToXMLOrdered([]byte(`[1,2,3]`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "JSON\u2192XML")
}

// TestJSONToXMLOrdered_ErrorOnScalar verifies that a top-level JSON scalar is
// rejected.
func TestJSONToXMLOrdered_ErrorOnScalar(t *testing.T) {
	_, err := jsonToXMLOrdered([]byte(`"hello"`))
	assert.Error(t, err)
}

// TestJSONToXMLOrdered_XMLNamespaceAttr verifies that an @xmlns key becomes
// an xmlns="..." attribute on the parent element rather than a child element.
func TestJSONToXMLOrdered_XMLNamespaceAttr(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"Add":{"@xmlns":"http://tempuri.org/","a":10,"b":5}}`))
	require.NoError(t, err)
	assert.Equal(t, `<Add xmlns="http://tempuri.org/"><a>10</a><b>5</b></Add>`, string(got))
}

// TestJSONToXMLOrdered_MultipleAttrs verifies multiple @-prefixed keys all
// become XML attributes in the order they appear.
func TestJSONToXMLOrdered_MultipleAttrs(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"El":{"@xmlns":"http://example.com/","@id":"42","value":"hello"}}`))
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, `xmlns="http://example.com/"`)
	assert.Contains(t, result, `id="42"`)
	assert.Contains(t, result, `<value>hello</value>`)
	assert.True(t, strings.HasPrefix(result, "<El "), "root element must be <El ...: %q", result)
}

// TestJSONToXMLOrdered_AttrSpecialCharsEscaped verifies that attribute values
// are XML-escaped (& → &amp;, " → &quot;).
func TestJSONToXMLOrdered_AttrSpecialCharsEscaped(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"El":{"@ns":"http://a.org/b&c","v":"x"}}`))
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, `ns="http://a.org/b&amp;c"`, "& in attr value must be escaped: %q", result)
}

// TestJSONToXMLOrdered_TopLevelAttrKeySkipped verifies that @-prefixed keys at
// the top level of the JSON object are silently ignored (no parent element to
// attach them to) and do not produce malformed XML tags.
func TestJSONToXMLOrdered_TopLevelAttrKeySkipped(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"@xmlns":"http://example.com/","a":"1"}`))
	require.NoError(t, err)
	assert.Equal(t, `<a>1</a>`, string(got))
}

// TestJSONToXMLOrdered_NumericAttrValue verifies that a numeric @-prefixed
// attribute value (e.g. @id:42) becomes id="42" rather than id="" (D1 fix).
func TestJSONToXMLOrdered_NumericAttrValue(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"El":{"@id":42,"v":"x"}}`))
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, `id="42"`, "numeric attribute must be non-empty: %q", result)
	assert.Contains(t, result, `<v>x</v>`)
}

// TestJSONToXMLOrdered_BoolAttrValue verifies that a boolean @-prefixed
// attribute value (e.g. @active:true) becomes active="true".
func TestJSONToXMLOrdered_BoolAttrValue(t *testing.T) {
	got, err := jsonToXMLOrdered([]byte(`{"El":{"@active":true,"v":"y"}}`))
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, `active="true"`, "boolean attribute must be non-empty: %q", result)
}
