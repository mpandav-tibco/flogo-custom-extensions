// Package wsdl provides types and utilities for parsing WSDL 1.1 documents.
// It supports both document-style and RPC-style SOAP 1.1 / 1.2 bindings.
package wsdl

import "encoding/xml"

// ---------------------------------------------------------------------------
// Top-level WSDL definitions
// ---------------------------------------------------------------------------

// Definitions is the root element of a WSDL 1.1 document.
type Definitions struct {
	XMLName         xml.Name   `xml:"definitions"`
	Name            string     `xml:"name,attr"`
	TargetNamespace string     `xml:"targetNamespace,attr"`
	Types           Types      `xml:"types"`
	Messages        []Message  `xml:"message"`
	PortTypes       []PortType `xml:"portType"`
	Bindings        []Binding  `xml:"binding"`
	Services        []Service  `xml:"service"`
	Imports         []Import   `xml:"import"`
}

// Import represents a <wsdl:import> element that references an external schema or WSDL.
type Import struct {
	Namespace string `xml:"namespace,attr"`
	Location  string `xml:"location,attr"`
}

// ---------------------------------------------------------------------------
// Types / embedded XSD
// ---------------------------------------------------------------------------

// Types holds the embedded XSD schema block(s).
type Types struct {
	Schemas []Schema `xml:"schema"`
}

// Schema is a minimal representation of an XSD <xs:schema> element sufficient
// for extracting element/complexType definitions needed for body-building.
type Schema struct {
	TargetNamespace string          `xml:"targetNamespace,attr"`
	Elements        []SchemaElement `xml:"element"`
	ComplexTypes    []ComplexType   `xml:"complexType"`
	SimpleTypes     []SimpleType    `xml:"simpleType"`
	Imports         []SchemaImport  `xml:"import"`
	// SourceURL is the URL or path from which this schema was fetched.
	// It is not an XML attribute — it is set by the parser after fetching so
	// that nested xs:import schemaLocation references can be resolved relative
	// to this schema's own location rather than always the root WSDL URL.
	SourceURL string `xml:"-"`
}

// SchemaImport represents <xs:import schemaLocation="..."/>.
type SchemaImport struct {
	Namespace      string `xml:"namespace,attr"`
	SchemaLocation string `xml:"schemaLocation,attr"`
}

// SchemaElement represents <xs:element name="..." type="..."/>.
type SchemaElement struct {
	Name        string      `xml:"name,attr"`
	Type        string      `xml:"type,attr"`
	MinOccurs   string      `xml:"minOccurs,attr"`
	MaxOccurs   string      `xml:"maxOccurs,attr"`
	ComplexType ComplexType `xml:"complexType"`
	SimpleType  SimpleType  `xml:"simpleType"`
}

// ComplexType represents <xs:complexType>.
type ComplexType struct {
	Name     string    `xml:"name,attr"`
	Sequence *Sequence `xml:"sequence"`
	All      *All      `xml:"all"`
	Choice   *Choice   `xml:"choice"`
	// ComplexContent / Extension supports xs:complexContent > xs:extension
	// which is used for type inheritance (very common in JAX-WS / WCF generated WSDLs).
	ComplexContent *ComplexContent `xml:"complexContent"`
}

// ComplexContent represents <xs:complexContent>.
type ComplexContent struct {
	Extension *Extension `xml:"extension"`
}

// Extension represents <xs:extension base="BaseType"> inside complexContent.
type Extension struct {
	Base     string    `xml:"base,attr"`
	Sequence *Sequence `xml:"sequence"`
	All      *All      `xml:"all"`
	Choice   *Choice   `xml:"choice"`
}

// Sequence represents <xs:sequence>.
type Sequence struct {
	Elements []SchemaElement `xml:"element"`
}

// All represents <xs:all>.
type All struct {
	Elements []SchemaElement `xml:"element"`
}

// Choice represents <xs:choice>.
type Choice struct {
	Elements []SchemaElement `xml:"element"`
}

// SimpleType represents <xs:simpleType>.
type SimpleType struct {
	Name        string      `xml:"name,attr"`
	Restriction Restriction `xml:"restriction"`
}

// Restriction represents <xs:restriction base="...">.
type Restriction struct {
	Base string `xml:"base,attr"`
}

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// Message represents a <wsdl:message> element.
type Message struct {
	Name  string        `xml:"name,attr"`
	Parts []MessagePart `xml:"part"`
}

// MessagePart represents a <wsdl:part> within a message.
type MessagePart struct {
	Name    string `xml:"name,attr"`
	Element string `xml:"element,attr"` // document-style: references xs:element
	Type    string `xml:"type,attr"`    // RPC-style: references xs:type
}

// ---------------------------------------------------------------------------
// PortType (abstract interface)
// ---------------------------------------------------------------------------

// PortType represents a <wsdl:portType>.
type PortType struct {
	Name       string        `xml:"name,attr"`
	Operations []PTOperation `xml:"operation"`
}

// PTOperation is a portType operation with input/output message references.
type PTOperation struct {
	Name          string      `xml:"name,attr"`
	Documentation string      `xml:"documentation"`
	Input         OperationIO `xml:"input"`
	Output        OperationIO `xml:"output"`
	Faults        []Fault     `xml:"fault"`
}

// OperationIO is either <wsdl:input> or <wsdl:output>.
type OperationIO struct {
	Name    string `xml:"name,attr"`
	Message string `xml:"message,attr"` // "tns:MessageName"
}

// Fault represents a <wsdl:fault>.
type Fault struct {
	Name    string `xml:"name,attr"`
	Message string `xml:"message,attr"`
}

// ---------------------------------------------------------------------------
// Binding (concrete protocol)
// ---------------------------------------------------------------------------

// Binding represents a <wsdl:binding> element.
type Binding struct {
	Name          string             `xml:"name,attr"`
	Type          string             `xml:"type,attr"` // references portType
	SOAPBinding   SOAPBinding        `xml:"http://schemas.xmlsoap.org/wsdl/soap/ binding"`
	SOAP12Binding SOAPBinding        `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ binding"`
	Operations    []BindingOperation `xml:"operation"`
}

// SOAPBinding holds <soap:binding> attributes.
type SOAPBinding struct {
	Style     string `xml:"style,attr"`     // "document" | "rpc"
	Transport string `xml:"transport,attr"` // e.g. HTTP transport URI
}

// BindingOperation is an <wsdl:operation> inside a binding.
type BindingOperation struct {
	Name          string        `xml:"name,attr"`
	SOAPOperation SOAPOperation `xml:"http://schemas.xmlsoap.org/wsdl/soap/ operation"`
	SOAP12Op      SOAPOperation `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ operation"`
	Input         BindingIO     `xml:"input"`
	Output        BindingIO     `xml:"output"`
}

// SOAPOperation holds <soap:operation soapAction="...">.
type SOAPOperation struct {
	SOAPAction string `xml:"soapAction,attr"`
	Style      string `xml:"style,attr"`
}

// BindingIO represents <wsdl:input> / <wsdl:output> inside a binding operation.
type BindingIO struct {
	SOAPBody   SOAPBody `xml:"http://schemas.xmlsoap.org/wsdl/soap/ body"`
	SOAP12Body SOAPBody `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ body"`
}

// SOAPBody holds <soap:body> attributes.
type SOAPBody struct {
	Use           string `xml:"use,attr"` // "literal" | "encoded"
	Namespace     string `xml:"namespace,attr"`
	EncodingStyle string `xml:"encodingStyle,attr"`
}

// ---------------------------------------------------------------------------
// Service / Port
// ---------------------------------------------------------------------------

// Service represents a <wsdl:service>.
type Service struct {
	Name  string `xml:"name,attr"`
	Ports []Port `xml:"port"`
}

// Port represents a <wsdl:port>.
type Port struct {
	Name        string      `xml:"name,attr"`
	Binding     string      `xml:"binding,attr"`
	SOAPAddress SOAPAddress `xml:"http://schemas.xmlsoap.org/wsdl/soap/ address"`
	SOAP12Addr  SOAPAddress `xml:"http://schemas.xmlsoap.org/wsdl/soap12/ address"`
}

// SOAPAddress holds <soap:address location="...">.
type SOAPAddress struct {
	Location string `xml:"location,attr"`
}

// ---------------------------------------------------------------------------
// Resolved / processed view — built by the parser after raw parsing
// ---------------------------------------------------------------------------

// Operation is the fully resolved view of a WSDL operation, combining
// portType, binding, and message information for easy consumption by the activity.
type Operation struct {
	Name        string // Operation name, e.g. "GetWeather"
	SOAPAction  string // Value for SOAPAction header, e.g. "http://service/GetWeather"
	Style       string // "document" | "rpc"
	BodyUse     string // "literal" | "encoded"
	SOAPVersion string // "1.1" | "1.2" — which binding this operation came from
	InputMsg    *ResolvedMessage
	OutputMsg   *ResolvedMessage
}

// ResolvedMessage is a message whose parts have been resolved to actual fields.
type ResolvedMessage struct {
	Name  string
	Parts []ResolvedPart
}

// ResolvedPart is a message part with its element/type fully resolved.
type ResolvedPart struct {
	Name        string     // Part name (often "parameters" for document-style)
	ElementName string     // Local name of the root element (document-style)
	ElementNS   string     // Namespace of the root element
	Fields      []FieldDef // Flattened fields from the element's complexType
}

// FieldDef describes a single field extracted from the XSD type tree.
type FieldDef struct {
	Name      string
	XSDType   string // xs:string, xs:int, etc.
	MinOccurs string // "0" or "1"
	MaxOccurs string // "1" or "unbounded"
	IsComplex bool
	Children  []FieldDef
}

// WSDLInfo is the summary returned by ParseWSDL to the activity.
type WSDLInfo struct {
	TargetNamespace string
	ServiceEndpoint string      // from <soap:address location>
	Operations      []Operation // all available operations
	// Warnings contains non-fatal parse issues (e.g. failed schema imports).
	// Callers should log these but they do not prevent operation discovery.
	Warnings []string
	// Raw holds the full parsed WSDL tree for introspection.
	// Set this field to nil after all derived values have been extracted
	// to allow the GC to reclaim the parse tree memory.
	Raw *Definitions
}
