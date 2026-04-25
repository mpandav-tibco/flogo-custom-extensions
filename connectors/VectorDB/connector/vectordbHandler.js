"use strict";
var __extends = this && this.__extends || function () { var t = function (e, i) { return (t = Object.setPrototypeOf || { __proto__: [] } instanceof Array && function (t, e) { t.__proto__ = e } || function (t, e) { for (var i in e) Object.prototype.hasOwnProperty.call(e, i) && (t[i] = e[i]) })(e, i) }; return function (e, i) { if ("function" != typeof i && null !== i) throw new TypeError("Class extends value " + String(i) + " is not a constructor or null"); function n() { this.constructor = e } t(e, i), e.prototype = null === i ? Object.create(i) : (n.prototype = i.prototype, new n) } }(),
    __decorate = this && this.__decorate || function (t, e, i, n) { var r, o = arguments.length, a = o < 3 ? e : null === n ? n = Object.getOwnPropertyDescriptor(e, i) : n; if ("object" == typeof Reflect && "function" == typeof Reflect.decorate) a = Reflect.decorate(t, e, i, n); else for (var s = t.length - 1; s >= 0; s--)(r = t[s]) && (a = (o < 3 ? r(a) : o > 3 ? r(e, i, a) : r(e, i)) || a); return o > 3 && a && Object.defineProperty(e, i, a), a },
    __metadata = this && this.__metadata || function (t, e) { if ("object" == typeof Reflect && "function" == typeof Reflect.metadata) return Reflect.metadata(t, e) };
Object.defineProperty(exports, "__esModule", { value: !0 }), exports.vectordbHandler = void 0;
var core_1 = require("@angular/core"),
    http_1 = require("@angular/http"),
    wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib"),

    // Fields that are only visible when useTLS=true
    TLS_FIELDS = ["tlsInsecureSkipVerify", "tlsServerName", "caCert", "clientCert", "clientKey"],

    // Fields visible only for a specific provider
    PROVIDER_FIELDS = {
        "qdrant": ["grpcPort"],
        "weaviate": ["scheme"],
        "milvus": ["username", "password", "dbName"],
        "chroma": []
    },

    // Fields that are only visible when enableEmbedding=true
    EMBEDDING_FIELDS = ["embeddingProvider", "embeddingAPIKey", "embeddingBaseURL"],

    vectordbHandler = function (t) {
        function e(e, i) {
            var n = t.call(this, e, i) || this;
            n.injector = e;
            n.http = i;
            n.value = function (t, e) { return null };
            n.validate = function (fieldName, ctx) {
                var useTLS = n.getContextVar(ctx, "useTLS");
                var dbType = n.getContextVar(ctx, "dbType");
                var enableEmbedding = n.getContextVar(ctx, "enableEmbedding");
                var tlsOn = useTLS === true || useTLS === "true";
                var embOn = enableEmbedding === true || enableEmbedding === "true";

                // --- TLS sub-fields: show only when useTLS is on ---
                if (TLS_FIELDS.indexOf(fieldName) !== -1) {
                    return wi_contrib_1.ValidationResult.newValidationResult().setVisible(tlsOn);
                }

                // --- Embedding sub-fields: show only when enableEmbedding is on ---
                if (EMBEDDING_FIELDS.indexOf(fieldName) !== -1) {
                    return wi_contrib_1.ValidationResult.newValidationResult().setVisible(embOn);
                }

                // --- Provider-specific fields: show only for the matching provider ---
                for (var provider in PROVIDER_FIELDS) {
                    if (PROVIDER_FIELDS[provider].indexOf(fieldName) !== -1) {
                        return wi_contrib_1.ValidationResult.newValidationResult().setVisible(dbType === provider);
                    }
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
exports.vectordbHandler = vectordbHandler;
