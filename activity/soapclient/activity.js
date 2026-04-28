"use strict";
var __extends = this && this.__extends || function () {
    var extendStatics = function (d, b) {
        return (extendStatics = Object.setPrototypeOf ||
            { __proto__: [] } instanceof Array && function (d, b) { d.__proto__ = b; } ||
            function (d, b) { for (var p in b) Object.prototype.hasOwnProperty.call(b, p) && (d[p] = b[p]); })(d, b);
    };
    return function (d, b) {
        if (typeof b !== "function" && b !== null)
            throw new TypeError("Class extends value " + String(b) + " is not a constructor or null");
        function __() { this.constructor = d; }
        extendStatics(d, b);
        d.prototype = b === null ? Object.create(b) : (__.prototype = b.prototype, new __());
    };
}();
var __decorate = this && this.__decorate || function (decorators, target, key, desc) {
    var c = arguments.length, r = c < 3 ? target : desc === null ? desc = Object.getOwnPropertyDescriptor(target, key) : desc, d;
    if (typeof Reflect === "object" && typeof Reflect.decorate === "function")
        r = Reflect.decorate(decorators, target, key, desc);
    else for (var i = decorators.length - 1; i >= 0; i--)
        if (d = decorators[i]) r = (c < 3 ? d(r) : c > 3 ? d(target, key, r) : d(target, key)) || r;
    return c > 3 && r && Object.defineProperty(target, key, r), r;
};
var __metadata = this && this.__metadata || function (k, v) {
    if (typeof Reflect === "object" && typeof Reflect.metadata === "function") return Reflect.metadata(k, v);
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.SoapClientActivityHandler = void 0;

var core_1 = require("@angular/core");
var http_1 = require("@angular/http");
var wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib");

// =============================================================================
// WSDL 1.1 browser-side parser
//
// Parses a WSDL XML document and produces:
//   operations       – [{unique_id, name}]  – for the operation dropdown
//   endpoint         – string               – SOAP service URL from <service>
//   operationDetails – { [opName]: {
//                          soapAction:  string,
//                          inputSchema: JSON-Schema-object | null
//                       }}
// =============================================================================

// ── DOM helper: find elements by localName, namespace-tolerant ────────────────
function byLocalName(parent, ns, localName) {
    var r = parent.getElementsByTagNameNS(ns, localName);
    if (r && r.length > 0) return r;
    r = parent.getElementsByTagName(localName);
    if (r && r.length > 0) return r;
    var pfx = ["wsdl", "xs", "xsd", "soap", "soap12", "s"];
    for (var i = 0; i < pfx.length; i++) {
        r = parent.getElementsByTagName(pfx[i] + ":" + localName);
        if (r && r.length > 0) return r;
    }
    return [];
}

// ── Cache key for a wsdlUrl field value ───────────────────────────────────────
function makeCacheKey(v) {
    if (!v) return null;
    // fileselector object: { content: "data:...;base64,...", filename: "..." }
    if (typeof v === "object") {
        var fn = v.filename || v.name || "";
        var len = (v.content || "").length;
        return fn + ":" + len;
    }
    if (typeof v === "string") {
        // Flogo fileselector JSON string: {"content":"data:...","filename":"foo.wsdl"}
        if (v.charAt(0) === "{") {
            try {
                var sel = JSON.parse(v);
                if (sel && sel.content) {
                    return (sel.filename || sel.name || "") + ":" + sel.content.length;
                }
            } catch (e) { }
        }
        return v.substring(0, 200);
    }
    return null;
}

// ── Decode wsdlUrl field value to XML text ────────────────────────────────────
// Returns { xmlText: string, isUrl: false }  for inline content
//      or { xmlText: null,   isUrl: true, url: string } for HTTP(S) URL
function decodeWsdlValue(v) {
    var none = { xmlText: null, isUrl: false };
    if (!v) return none;
    console.log("[SoapClient] decodeWsdlValue type=", typeof v,
        typeof v === "string" ? "len=" + v.length + " starts=" + v.substring(0, 30) : "");

    // ── Object: Flogo fileselector passes {content, filename} ──────────────
    if (typeof v === "object" && v.content) {
        var raw = v.content;
        // Case A: content is already raw XML text (Flogo readAsText path)
        if (typeof raw === "string" && raw.replace(/^[\s\uFEFF\xEF\xBB\xBF]+/, "").charAt(0) === "<") {
            console.log("[SoapClient] decodeWsdlValue: object.content is raw XML text");
            return { xmlText: raw, isUrl: false };
        }
        // Case B: content is a data-URI (data:...;base64,XXX) or plain base64
        try {
            var commaIdx = raw.indexOf(",");
            var b64 = commaIdx >= 0 ? raw.slice(commaIdx + 1) : raw;
            var decoded = atob(b64);
            console.log("[SoapClient] decodeWsdlValue: object.content decoded, first char code:", decoded.charCodeAt(0));
            return { xmlText: decoded, isUrl: false };
        } catch (e) {
            console.warn("[SoapClient] decodeWsdlValue: atob failed for object.content", e.message);
            return none;
        }
    }

    if (typeof v === "string") {
        // Flogo fileselector JSON string: {"content":"data:...;base64,...","filename":"foo.wsdl"}
        if (v.charAt(0) === "{") {
            try {
                var sel = JSON.parse(v);
                if (sel && sel.content) {
                    // Check if content is raw XML
                    if (sel.content.replace(/^[\s\uFEFF\xEF\xBB\xBF]+/, "").charAt(0) === "<") {
                        return { xmlText: sel.content, isUrl: false };
                    }
                    var commaIdx = sel.content.indexOf(",");
                    var b64 = commaIdx >= 0 ? sel.content.slice(commaIdx + 1) : sel.content;
                    return { xmlText: atob(b64), isUrl: false };
                }
            } catch (e) { }
        }
        // Raw XML text passed directly (starts with < after stripping BOM/whitespace)
        if (v.replace(/^[\s\uFEFF\xEF\xBB\xBF]+/, "").charAt(0) === "<") {
            console.log("[SoapClient] decodeWsdlValue: string value is raw XML");
            return { xmlText: v, isUrl: false };
        }
        // base64 data-URI
        if (v.indexOf("data:") === 0) {
            try {
                var b64 = v.slice(v.indexOf(",") + 1);
                return { xmlText: atob(b64), isUrl: false };
            } catch (e) { return none; }
        }
        // Remote HTTP/HTTPS URL
        if (v.match(/^https?:\/\//i)) {
            return { xmlText: null, isUrl: true, url: v };
        }
        // Plain base64 string (no prefix)
        if (/^[A-Za-z0-9+/]+=*$/.test(v.substring(0, 64))) {
            try { return { xmlText: atob(v), isUrl: false }; } catch (e) { }
        }
    }
    return none;
}

// ── Full WSDL 1.1 parser ──────────────────────────────────────────────────────
function parseWSDL(xmlText) {
    var result = { operations: [], endpoint: "", operationDetails: {} };
    try {
        // Step 1: Strip BOM bytes and leading whitespace.
        var sanitized = xmlText
            .replace(/^(\xEF\xBB\xBF|\xFF\xFE|\xFE\xFF|\uFEFF)/, "")
            .replace(/^\s+/, "");

        // Step 2: If content doesn't start with '<', the file may be Chrome's
        // "Save As > Webpage" rendering of XML, which prepends the text:
        //   "This XML file does not appear to have any style information..."
        // and may HTML-entity-encode the actual XML ('<' → '&lt;').
        // Try to recover the actual XML content.
        if (sanitized.charAt(0) !== "<") {
            console.warn("[SoapClient] parseWSDL: content does not start with '<'. First 200 chars:\n", sanitized.substring(0, 200));
            var xmlStart = -1;

            // Check for HTML-entity-encoded XML (Chrome "Webpage, HTML Only" save)
            if (sanitized.indexOf("&lt;?xml") >= 0 || sanitized.indexOf("&lt;wsdl:") >= 0 ||
                sanitized.indexOf("&lt;definitions") >= 0) {
                try {
                    var ta = document.createElement("textarea");
                    ta.innerHTML = sanitized;
                    sanitized = ta.value.replace(/^\s+/, "");
                    xmlStart = sanitized.indexOf("<?xml");
                    if (xmlStart < 0) xmlStart = sanitized.search(/<(?:wsdl:|definitions)\b/i);
                    console.log("[SoapClient] parseWSDL: HTML-unescaped; xmlStart=", xmlStart,
                        "first 60:", sanitized.substring(xmlStart < 0 ? 0 : xmlStart, (xmlStart < 0 ? 0 : xmlStart) + 60));
                } catch (e) { xmlStart = -1; }
            }

            // Try literal <?xml or root WSDL element
            if (xmlStart < 0) xmlStart = sanitized.indexOf("<?xml");
            if (xmlStart < 0) xmlStart = sanitized.search(/<(?:wsdl:|definitions)\b/i);

            if (xmlStart >= 0) {
                sanitized = sanitized.slice(xmlStart);
                console.log("[SoapClient] parseWSDL: extracted XML at offset", xmlStart,
                    "first 60:", sanitized.substring(0, 60));
            } else {
                // Cannot recover XML — the file is Chrome's HTML snapshot and the
                // original XML source is not embedded in recoverable form.
                // The user must download the raw .wsdl file (e.g. via curl/wget).
                console.error("[SoapClient] parseWSDL: no XML found in uploaded file.",
                    "The file appears to be Chrome's HTML rendering of the WSDL page.",
                    "Please download the raw WSDL using: curl -o file.wsdl \"<WSDL URL>\"");
                return result;
            }
        }

        // Step 3: Strip the encoding attribute — non-UTF-8 declarations (e.g.
        // ISO-8859-1) cause Chromium's DOMParser to reject the document.
        sanitized = sanitized.replace(/(<\?xml\b[^?]*?)\s+encoding\s*=\s*["'][^"']*["']/i, "$1");
        console.log("[SoapClient] parseWSDL sanitized first 60:", sanitized.substring(0, 60));
        var doc = (new DOMParser()).parseFromString(sanitized, "text/xml");

        // Detect a DOMParser parse error and abort gracefully.
        if (doc.getElementsByTagName("parsererror").length > 0) {
            console.error("[SoapClientHandler] WSDL XML parse error:",
                doc.getElementsByTagName("parsererror")[0].textContent);
            return result;
        }

        var WSDL = "http://schemas.xmlsoap.org/wsdl/";
        var SOAP11 = "http://schemas.xmlsoap.org/wsdl/soap/";
        var SOAP12 = "http://schemas.xmlsoap.org/wsdl/soap12/";
        var XS = "http://www.w3.org/2001/XMLSchema";

        // ── Step 1: SOAP endpoint from <service><port><soap:address location="..."> ──
        (function findEndpoint() {
            var svcs = byLocalName(doc, WSDL, "service");
            for (var s = 0; s < svcs.length; s++) {
                var ports = byLocalName(svcs[s], WSDL, "port");
                for (var p = 0; p < ports.length; p++) {
                    var candidates = [
                        ports[p].getElementsByTagNameNS(SOAP11, "address"),
                        ports[p].getElementsByTagNameNS(SOAP12, "address"),
                        ports[p].getElementsByTagName("soap:address"),
                        ports[p].getElementsByTagName("soap12:address"),
                        ports[p].getElementsByTagName("address")
                    ];
                    for (var c = 0; c < candidates.length; c++) {
                        if (candidates[c].length > 0) {
                            var loc = candidates[c][0].getAttribute("location");
                            if (loc) { result.endpoint = loc; return; }
                        }
                    }
                }
            }
        })();

        // ── Step 2: soapAction per operation from <binding><operation><soap:operation> ──
        var soapActions = {};
        var bindings = byLocalName(doc, WSDL, "binding");
        for (var b = 0; b < bindings.length; b++) {
            var bOps = byLocalName(bindings[b], WSDL, "operation");
            for (var o = 0; o < bOps.length; o++) {
                var opName = bOps[o].getAttribute("name");
                if (!opName) continue;
                if (soapActions.hasOwnProperty(opName)) continue;
                var soapOpCands = [
                    bOps[o].getElementsByTagNameNS(SOAP11, "operation"),
                    bOps[o].getElementsByTagNameNS(SOAP12, "operation"),
                    bOps[o].getElementsByTagName("soap:operation"),
                    bOps[o].getElementsByTagName("soap12:operation")
                ];
                var soapAction = "";
                for (var c = 0; c < soapOpCands.length; c++) {
                    if (soapOpCands[c].length > 0) {
                        soapAction = soapOpCands[c][0].getAttribute("soapAction") || "";
                        break;
                    }
                }
                soapActions[opName] = soapAction;
            }
        }

        // ── Step 3: portType operation → input AND output message names ──────
        var opInputMsg = {};
        var opOutputMsg = {};
        var portTypes = byLocalName(doc, WSDL, "portType");
        for (var p = 0; p < portTypes.length; p++) {
            var ptOps = byLocalName(portTypes[p], WSDL, "operation");
            for (var o = 0; o < ptOps.length; o++) {
                var opName = ptOps[o].getAttribute("name");
                var inEls = byLocalName(ptOps[o], WSDL, "input");
                if (opName && inEls.length > 0) {
                    var ref = (inEls[0].getAttribute("message") || "").split(":").pop();
                    opInputMsg[opName] = ref;
                }
                var outEls = byLocalName(ptOps[o], WSDL, "output");
                if (opName && outEls.length > 0) {
                    var outRef = (outEls[0].getAttribute("message") || "").split(":").pop();
                    opOutputMsg[opName] = outRef;
                }
            }
        }

        // ── Step 4: message name → parts (supports both document-style
        //           element refs and RPC-style type refs) ───────────────────
        var msgParts = {};
        var messages = byLocalName(doc, WSDL, "message");
        for (var m = 0; m < messages.length; m++) {
            var msgName = messages[m].getAttribute("name");
            if (!msgName) continue;
            var parts = byLocalName(messages[m], WSDL, "part");
            var partsArr = [];
            for (var pp = 0; pp < parts.length; pp++) {
                var elAttr = (parts[pp].getAttribute("element") || "").split(":").pop();
                var typeAttr = (parts[pp].getAttribute("type") || "").split(":").pop();
                partsArr.push({
                    name: parts[pp].getAttribute("name") || "parameters",
                    element: elAttr,
                    type: typeAttr
                });
            }
            msgParts[msgName] = partsArr;
        }

        // ── Step 5: xs:element → JSON Schema (object with typed properties) ────

        function xsType2json(xsType) {
            var t = ((xsType || "string").split(":").pop()).toLowerCase();
            if (t === "int" || t === "integer" || t === "long" ||
                t === "short" || t === "byte" || t === "decimal" ||
                t === "float" || t === "double" || t === "unsignedint" ||
                t === "unsignedlong" || t === "nonnegativeinteger") return "number";
            if (t === "boolean") return "boolean";
            return "string";
        }

        function buildSchemaFromEl(topEl) {
            var props = {};
            var required = [];

            var complexEls = topEl.getElementsByTagNameNS(XS, "complexType");
            if (!complexEls || complexEls.length === 0)
                complexEls = topEl.getElementsByTagName("xs:complexType");
            if (!complexEls || complexEls.length === 0)
                complexEls = topEl.getElementsByTagName("xsd:complexType");

            if (complexEls && complexEls.length > 0) {
                var childEls = complexEls[0].getElementsByTagNameNS(XS, "element");
                if (!childEls || childEls.length === 0)
                    childEls = complexEls[0].getElementsByTagName("xs:element");
                if (!childEls || childEls.length === 0)
                    childEls = complexEls[0].getElementsByTagName("xsd:element");

                for (var f = 0; f < childEls.length; f++) {
                    var fname = childEls[f].getAttribute("name");
                    var ftype = childEls[f].getAttribute("type") || "string";
                    if (!fname) continue;
                    props[fname] = { type: xsType2json(ftype) };
                    var minOcc = childEls[f].getAttribute("minOccurs");
                    if (minOcc === null || (minOcc !== "0")) required.push(fname);
                }
            }

            var schema = {
                "$schema": "http://json-schema.org/draft-07/schema#",
                "type": "object",
                "properties": props
            };
            if (required.length > 0) schema.required = required;
            return schema;
        }

        // Collect top-level xs:element nodes (direct children of xs:schema)
        var topLevelElements = {};
        (function collectTopLevelElements() {
            function process(els) {
                for (var i = 0; i < els.length; i++) {
                    var par = els[i].parentNode;
                    if (par && par.localName === "schema") {
                        var n = els[i].getAttribute("name");
                        if (n && !topLevelElements[n])
                            topLevelElements[n] = buildSchemaFromEl(els[i]);
                    }
                }
            }
            process(doc.getElementsByTagNameNS(XS, "element"));
            process(doc.getElementsByTagName("xs:element"));
            process(doc.getElementsByTagName("xsd:element"));
        })();

        // ── Step 6: Assemble result ────────────────────────────────────────────
        var seen = {};

        function buildSchemaFromParts(parts, title) {
            var inputSchema = null;
            if (parts.length === 1 && parts[0].element) {
                // Document-style: single part with element reference
                if (topLevelElements[parts[0].element])
                    inputSchema = topLevelElements[parts[0].element];
            } else if (parts.length > 0) {
                // RPC-style (or multi-part)
                var props = {};
                var required = [];
                for (var pp = 0; pp < parts.length; pp++) {
                    var pName = parts[pp].name;
                    var pType = parts[pp].type || parts[pp].element || "string";
                    if (topLevelElements[pType]) {
                        props[pName] = topLevelElements[pType];
                    } else {
                        props[pName] = { type: xsType2json(pType) };
                    }
                    required.push(pName);
                }
                inputSchema = {
                    "$schema": "http://json-schema.org/draft-07/schema#",
                    "title": title,
                    "type": "object",
                    "properties": props,
                    "required": required
                };
            }
            return inputSchema;
        }

        function addOp(opName) {
            if (seen[opName]) return;
            seen[opName] = true;
            result.operations.push(opName);

            // Input (request) schema
            var inMsgName = opInputMsg[opName];
            var inParts = inMsgName ? (msgParts[inMsgName] || []) : [];
            var inputSchema = buildSchemaFromParts(inParts, opName);

            // Output (response) schema
            var outMsgName = opOutputMsg[opName];
            var outParts = outMsgName ? (msgParts[outMsgName] || []) : [];
            var outputSchema = buildSchemaFromParts(outParts, opName + "Response");

            result.operationDetails[opName] = {
                soapAction: soapActions[opName] || "",
                inputSchema: inputSchema,
                outputSchema: outputSchema
            };
        }

        // Prefer binding operations (they carry soapAction)
        for (var opName in soapActions) addOp(opName);
        // Fall back to portType operations if bindings produced nothing
        if (result.operations.length === 0)
            for (var opName in opInputMsg) addOp(opName);

    } catch (ex) {
        console.error("[SoapClientHandler] WSDL parse error:", ex);
    }
    return result;
}

// Fixed SOAP 1.1 Fault JSON Schema.
// Used for soapResponseFault — covers both SOAP 1.1 and 1.2 faults since all
// faults are surfaced via the same output field using the SOAP 1.1 convention.
var SOAP11_FAULT_SCHEMA = JSON.stringify({
    "$schema": "http://json-schema.org/draft-07/schema#",
    "title": "SOAPFault",
    "description": "SOAP 1.1 Fault structure. Present when isFault=true.",
    "type": "object",
    "properties": {
        "faultcode": { "type": "string", "description": "Fault code, e.g. soap:Server or soap:Client" },
        "faultstring": { "type": "string", "description": "Human-readable description of the fault" },
        "faultactor": { "type": "string", "description": "URI of the node that generated the fault (optional)" },
        "detail": { "type": "object", "description": "Service-specific fault detail element (optional)" }
    },
    "required": ["faultcode", "faultstring"]
});

// =============================================================================
// Angular / Flogo UI activity handler
// =============================================================================

var SoapClientActivityHandler = function (base) {

    function SoapClientHandler(injector, http) {
        var self = base.call(this, injector, http) || this;
        self.injector = injector;
        self.http = http;
        self._cache = {};  // makeCacheKey(wsdlVal) → parseWSDL result

        // ── value() ──────────────────────────────────────────────────────────
        // Flogo calls this to get dropdown options, field defaults, and dynamic
        // JSON schemas for "any"-typed input/output fields.
        self.value = function (fieldName, ctx) {
            console.log("[SoapClient] value(", fieldName, ") src=", self._get(ctx, "wsdlSourceType"),
                "hasWsdl=", self._hasWsdl(ctx));

            // Populate wsdlOperation dropdown with operations from the WSDL.
            // Return null (not []) when there is no WSDL so the field is fully reset.
            if (fieldName === "wsdlOperation") {
                if (!self._hasWsdl(ctx)) return null;
                return self._withParsed(ctx, function (parsed) {
                    console.log("[SoapClient] wsdlOperation parsed ops:", parsed ? parsed.operations : null);
                    return parsed ? parsed.operations : null;
                });
            }

            // Auto-populate SOAP service endpoint from the WSDL <service> element.
            // Only overrides when WSDL is present; returns null otherwise (no override).
            if (fieldName === "soapServiceEndpoint") {
                if (!self._hasWsdl(ctx)) return null;
                return self._withParsed(ctx, function (parsed) {
                    return (parsed && parsed.endpoint) ? parsed.endpoint : null;
                });
            }

            // Auto-populate SOAPAction input from WSDL binding.
            // Reset to null when no WSDL so the field is cleared.
            if (fieldName === "soapAction") {
                if (!self._hasWsdl(ctx)) return null;
                return self._withParsedOp(ctx, function (details) {
                    return (details && details.soapAction) ? details.soapAction : null;
                });
            }

            // Return JSON Schema for the selected operation's input message.
            // Rules:
            //   xmlMode=true                     → "{}" — visible, untyped (user supplies a JSON object; @-prefixed keys become XML attributes)
            //   xmlMode=false + WSDL op selected → typed schema from WSDL input message
            //   xmlMode=false + no WSDL / no op  → "{}" — visible, untyped (no schema available yet)
            if (fieldName === "soapRequestBody") {
                var xmlMode = self._get(ctx, "xmlMode");
                if (xmlMode === true || xmlMode === "true") {
                    return "{}";
                }
                return self._withParsedOp(ctx, function (details) {
                    return (details && details.inputSchema)
                        ? JSON.stringify(details.inputSchema)
                        : "{}";
                });
            }

            // Return JSON Schema for the selected operation's output (response) message.
            // Rules:
            //   xmlMode=true                     → "{}" — visible, untyped (response is raw XML)
            //   xmlMode=false + WSDL op selected → typed schema from WSDL output message
            //   xmlMode=false + no WSDL / no op  → "{}" — visible, untyped (no schema available yet)
            if (fieldName === "soapResponsePayload") {
                var xmlMode = self._get(ctx, "xmlMode");
                if (xmlMode === true || xmlMode === "true") {
                    return "{}";
                }
                return self._withParsedOp(ctx, function (details) {
                    return (details && details.outputSchema)
                        ? JSON.stringify(details.outputSchema)
                        : "{}";
                });
            }

            // Return fixed SOAP 1.1 Fault JSON Schema for the fault output field.
            // This is unconditional — every SOAP service can return a fault.
            if (fieldName === "soapResponseFault") {
                return SOAP11_FAULT_SCHEMA;
            }

            // Populate authorizationConn dropdown with all HTTP auth connections.
            if (fieldName === "authorizationConn") {
                var conns = [];
                wi_contrib_1.WiContributionUtils.getConnections(self.http, "General").subscribe(
                    function (list) {
                        list.forEach(function (c) {
                            for (var j = 0; j < c.settings.length; j++) {
                                if (c.settings[j].name === "name") {
                                    conns.push({
                                        unique_id: wi_contrib_1.WiContributionUtils.getUniqueId(c),
                                        name: c.settings[j].value
                                    });
                                    break;
                                }
                            }
                        });
                    },
                    function (err) {
                        console.warn("[SoapClient] getConnections error:", err);
                    }
                );
                return conns;
            }

            return null;
        };

        // ── validate() ───────────────────────────────────────────────────────
        // Controls field visibility and read-only state.
        self.validate = function (fieldName, ctx) {

            // Show wsdlUrl fileselector only when "WSDL File" is selected
            if (fieldName === "wsdlUrl") {
                var src = self._get(ctx, "wsdlSourceType");
                return wi_contrib_1.ValidationResult.newValidationResult().setVisible(src === "WSDL File");
            }

            // Show wsdlHttpUrl text field only when "WSDL URL" is selected.
            // Also warn the user that browser CORS policy blocks design-time WSDL fetching.
            if (fieldName === "wsdlHttpUrl") {
                var src = self._get(ctx, "wsdlSourceType");
                var visible = src === "WSDL URL";
                var vr = wi_contrib_1.ValidationResult.newValidationResult().setVisible(visible);
                if (visible) {
                    var u = self._get(ctx, "wsdlHttpUrl");
                    if (u && typeof u === "string" && u.match(/^https?:\/\//i)) {
                        // Design-time WSDL fetch from a browser webview is blocked by CORS.
                        // Return a warning so the user knows operations won't auto-populate.
                        vr.setError("CORS_WARN",
                            "Browser CORS policy prevents fetching this URL in the designer. " +
                            "Operations and body schema will NOT auto-populate. " +
                            "For design-time features, use \"WSDL File\" instead. " +
                            "The URL is still used by the runtime.");
                    }
                }
                return vr;
            }

            // TLS-dependent fields: visible only when enableTLS is true
            if (fieldName === "skipTlsVerify" ||
                fieldName === "serverCertificate" ||
                fieldName === "clientCertificate" ||
                fieldName === "clientKey") {
                var tlsField = ctx.getField("enableTLS");
                var tlsOn = tlsField && (tlsField.value === true || tlsField.value === "true");
                return wi_contrib_1.ValidationResult.newValidationResult().setVisible(!!tlsOn);
            }

            // WSDL-dependent fields: visible once a WSDL source is configured
            if (fieldName === "wsdlOperation" || fieldName === "autoUseWsdlEndpoint") {
                return wi_contrib_1.ValidationResult.newValidationResult().setVisible(self._hasWsdl(ctx));
            }

            // Show authorizationConn connection picker only when authorization is enabled.
            if (fieldName === "authorizationConn") {
                var authField = ctx.getField("authorization");
                var authOn = authField && (authField.value === true || authField.value === "true");
                return wi_contrib_1.ValidationResult.newValidationResult().setVisible(!!authOn);
            }

            return null;
        };

        // ── action() ─────────────────────────────────────────────────────────
        self.action = function (actionName, ctx) { return null; };

        return self;
    }

    __extends(SoapClientHandler, base);

    // ── Safely read a field's value ───────────────────────────────────────────
    SoapClientHandler.prototype._get = function (ctx, name) {
        var f = ctx.getField(name);
        return (f && f.value !== undefined) ? f.value : null;
    };

    // ── True when any WSDL source has meaningful content ────────────────────────
    SoapClientHandler.prototype._hasWsdl = function (ctx) {
        var src = this._get(ctx, "wsdlSourceType");
        if (src === "WSDL URL") {
            var u = this._get(ctx, "wsdlHttpUrl");
            return !!(u && typeof u === "string" && u.trim().length > 0);
        }
        if (src === "WSDL File") {
            var v = this._get(ctx, "wsdlUrl");
            if (!v) return false;
            if (typeof v === "object" && v.content) return true;
            if (typeof v === "string" && v.trim().length > 0) return true;
        }
        return false;
    };

    // ── Resolve and cache a parsed WSDL, then invoke cb(parsedResult|null) ───
    // Returns the cb return value directly for synchronous (file) content, or
    // an Observable for asynchronous (HTTP) fetches.
    SoapClientHandler.prototype._withParsed = function (ctx, cb) {
        var self = this;
        // Determine source based on the dropdown selection
        var src = self._get(ctx, "wsdlSourceType");
        var wsdlVal = null;
        if (src === "WSDL URL") {
            var u = self._get(ctx, "wsdlHttpUrl");
            wsdlVal = (u && typeof u === "string" && u.trim()) ? u.trim() : null;
        } else if (src === "WSDL File") {
            wsdlVal = self._get(ctx, "wsdlUrl");
        }
        if (!wsdlVal) {
            // Source cleared or set to "none" — evict entire cache so stale
            // entries do not survive if the user re-loads a different WSDL later.
            self._cache = {};
            return cb(null);
        }

        var key = makeCacheKey(wsdlVal);
        if (key && self._cache[key]) return cb(self._cache[key]);

        var decoded = decodeWsdlValue(wsdlVal);

        // Inline content decoded from a browsed file
        if (decoded.xmlText) {
            var parsed = parseWSDL(decoded.xmlText);
            if (key) self._cache[key] = parsed;
            return cb(parsed);
        }

        // Remote HTTP/HTTPS URL — browser CORS prevents fetching in the designer.
        // URL-based WSDLs are used at runtime (Go) only; return null here.
        if (decoded.isUrl) {
            console.warn("[SoapClient] WSDL URL fetch skipped in designer (CORS policy). Runtime will use this URL.");
            return cb(null);
        }

        return cb(null);
    };

    // ── Like _withParsed but also resolves the currently selected operation ──
    SoapClientHandler.prototype._withParsedOp = function (ctx, cb) {
        var self = this;
        var opName = self._get(ctx, "wsdlOperation");
        return self._withParsed(ctx, function (parsed) {
            if (!parsed || !opName) return cb(null);
            return cb(parsed.operationDetails[opName] || null);
        });
    };

    SoapClientHandler = __decorate([
        wi_contrib_1.WiContrib({}),
        core_1.Injectable(),
        __metadata("design:paramtypes", [core_1.Injector, http_1.Http])
    ], SoapClientHandler);

    return SoapClientHandler;
}(wi_contrib_1.WiServiceHandlerContribution);

exports.SoapClientActivityHandler = SoapClientActivityHandler;
