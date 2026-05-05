"use strict";
var __extends = this && this.__extends || function () { var t = function (e, i) { return (t = Object.setPrototypeOf || { __proto__: [] } instanceof Array && function (t, e) { t.__proto__ = e } || function (t, e) { for (var i in e) Object.prototype.hasOwnProperty.call(e, i) && (t[i] = e[i]) })(e, i) }; return function (e, i) { if ("function" != typeof i && null !== i) throw new TypeError("Class extends value " + String(i) + " is not a constructor or null"); function n() { this.constructor = e } t(e, i), e.prototype = null === i ? Object.create(i) : (n.prototype = i.prototype, new n) } }(),
    __decorate = this && this.__decorate || function (t, e, i, n) { var r, o = arguments.length, a = o < 3 ? e : null === n ? n = Object.getOwnPropertyDescriptor(e, i) : n; if ("object" == typeof Reflect && "function" == typeof Reflect.decorate) a = Reflect.decorate(t, e, i, n); else for (var s = t.length - 1; s >= 0; s--)(r = t[s]) && (a = (o < 3 ? r(a) : o > 3 ? r(e, i, a) : r(e, i)) || a); return o > 3 && a && Object.defineProperty(e, i, a), a },
    __metadata = this && this.__metadata || function (t, e) { if ("object" == typeof Reflect && "function" == typeof Reflect.metadata) return Reflect.metadata(t, e) };
Object.defineProperty(exports, "__esModule", { value: !0 }), exports.IngestDocumentsActivityHandler = void 0;
var core_1 = require("@angular/core"),
    http_1 = require("@angular/http"),
    wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib"),

    // These fields are hidden when useConnectorEmbedding=true (inherited from connector)
    CONNECTOR_INHERITED_FIELDS = ["embeddingProvider", "embeddingAPIKey", "embeddingBaseURL"],
    // All chunking sub-fields: hidden when enableChunking=false
    CHUNKING_SUB_FIELDS = ["chunkStrategy", "chunkSize", "chunkOverlap"],

    IngestDocumentsActivityHandler = function (t) {
        function e(e, i) {
            var n = t.call(this, e, i) || this;
            n.injector = e;
            n.http = i;
            n.value = function (t, e) { return null };
            n.validate = function (fieldName, ctx) {
                var useConnector = n.getContextVar(ctx, "useConnectorEmbedding");
                var inherit = useConnector === true || useConnector === "true";

                // --- Embedding credential fields: hide when connector-level settings are in use ---
                if (CONNECTOR_INHERITED_FIELDS.indexOf(fieldName) !== -1) {
                    return wi_contrib_1.ValidationResult.newValidationResult().setVisible(!inherit);
                }

                // --- Chunking sub-fields: only visible when enableChunking=true ---
                if (CHUNKING_SUB_FIELDS.indexOf(fieldName) !== -1) {
                    var enableChunking = n.getContextVar(ctx, "enableChunking");
                    var chunkingOn = enableChunking === true || enableChunking === "true";

                    // chunkOverlap is only relevant for the "fixed" strategy
                    if (fieldName === "chunkOverlap") {
                        var strategy = n.getContextVar(ctx, "chunkStrategy");
                        return wi_contrib_1.ValidationResult.newValidationResult().setVisible(chunkingOn && strategy === "fixed");
                    }

                    // chunkSize is relevant for "fixed" and "sentence" only
                    // (paragraph and heading derive their own split boundaries)
                    if (fieldName === "chunkSize") {
                        var strategy = n.getContextVar(ctx, "chunkStrategy");
                        var sizeRelevant = !strategy || strategy === "fixed" || strategy === "sentence";
                        return wi_contrib_1.ValidationResult.newValidationResult().setVisible(chunkingOn && sizeRelevant);
                    }

                    // chunkStrategy is always visible when chunking is on
                    return wi_contrib_1.ValidationResult.newValidationResult().setVisible(chunkingOn);
                }

                return null;
            };
            n.action = function (t, e) { return null };
            return n;
        }
        __extends(e, t);
        e.prototype.getContextVar = function (ctx, name) {
            return ctx.getField(name) ? void 0 === ctx.getField(name).value ? "" : ctx.getField(name).value : "";
        };
        e = __decorate([wi_contrib_1.WiContrib({}), core_1.Injectable(), __metadata("design:paramtypes", [core_1.Injector, http_1.Http])], e);
        return e;
    }(wi_contrib_1.WiServiceHandlerContribution);
exports.IngestDocumentsActivityHandler = IngestDocumentsActivityHandler;
