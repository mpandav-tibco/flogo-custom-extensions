"use strict";
var __extends = this && this.__extends || function () { var t = function (e, i) { return (t = Object.setPrototypeOf || { __proto__: [] } instanceof Array && function (t, e) { t.__proto__ = e } || function (t, e) { for (var i in e) Object.prototype.hasOwnProperty.call(e, i) && (t[i] = e[i]) })(e, i) }; return function (e, i) { if ("function" != typeof i && null !== i) throw new TypeError("Class extends value " + String(i) + " is not a constructor or null"); function n() { this.constructor = e } t(e, i), e.prototype = null === i ? Object.create(i) : (n.prototype = i.prototype, new n) } }(),
    __decorate = this && this.__decorate || function (t, e, i, n) { var r, o = arguments.length, a = o < 3 ? e : null === n ? n = Object.getOwnPropertyDescriptor(e, i) : n; if ("object" == typeof Reflect && "function" == typeof Reflect.decorate) a = Reflect.decorate(t, e, i, n); else for (var s = t.length - 1; s >= 0; s--)(r = t[s]) && (a = (o < 3 ? r(a) : o > 3 ? r(e, i, a) : r(e, i)) || a); return o > 3 && a && Object.defineProperty(e, i, a), a },
    __metadata = this && this.__metadata || function (t, e) { if ("object" == typeof Reflect && "function" == typeof Reflect.metadata) return Reflect.metadata(t, e) };
Object.defineProperty(exports, "__esModule", { value: !0 }), exports.vectordbHandler = void 0;
var core_1 = require("@angular/core"),
    http_1 = require("@angular/http"),
    wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib"),
    contrib_1 = require("wi-studio/common/models/contrib"),
    rxjs_extensions_1 = require("wi-studio/common/rxjs-extensions"),
    validation_1 = require("wi-studio/common/models/validation"),

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
            // Converts the settings array [{name, value}, ...] to a plain object
            n.toSettingsObj = function (settingsArr) {
                return rxjs_extensions_1.Observable.from(settingsArr)
                    .reduce(function (acc, field) { acc[field.name] = field.value; return acc; }, {});
            };

            n.action = function (actionName, connCtx) {
                if (actionName !== "Connect") { return null; }

                return n.toSettingsObj(connCtx.settings).switchMap(function (s) {
                    var host = s.host || "localhost";
                    var port = parseInt(s.port, 10) || 0;
                    var dbType = (s.dbType || "").toLowerCase();
                    var useTLS = s.useTLS === true || s.useTLS === "true";
                    var scheme = s.scheme || (useTLS ? "https" : "http");

                    // Provider default ports when port is 0
                    if (port <= 0) {
                        var defaults = { "weaviate": 8080, "qdrant": 6333, "chroma": 8000, "milvus": 19530 };
                        port = defaults[dbType] || 8080;
                    }

                    // Health endpoint per provider
                    var paths = { "weaviate": "/v1/.well-known/ready", "qdrant": "/healthz", "chroma": "/api/v1/heartbeat", "milvus": "/healthz" };
                    var url = scheme + "://" + host + ":" + port + (paths[dbType] || "/healthz");

                    // Use native fetch with mode:"no-cors" so Weaviate's missing CORS headers
                    // don't block the request in VS Code's webview/Electron context.
                    // An opaque response = server reachable; TypeError = server down.
                    return new rxjs_extensions_1.Observable(function (observer) {
                        window.fetch(url, { method: "GET", mode: "no-cors", cache: "no-cache" })
                            .then(function (_resp) {
                                observer.next(
                                    contrib_1.ActionResult.newActionResult()
                                        .setResult({ context: connCtx, authType: wi_contrib_1.AUTHENTICATION_TYPE.BASIC, authData: {} })
                                );
                                observer.complete();
                            })
                            .catch(function (err) {
                                var msg = (err && err.message) ? err.message : ("unreachable: " + url);
                                observer.next(
                                    contrib_1.ActionResult.newActionResult()
                                        .setSuccess(false)
                                        .setResult(new validation_1.ValidationError("VDB-1001", "Connection failed: " + msg))
                                );
                                observer.complete();
                            });
                    });
                });
            };
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
