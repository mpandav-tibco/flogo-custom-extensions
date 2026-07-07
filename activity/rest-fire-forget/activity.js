"use strict";
// =============================================================================
// REST Fire & Forget — Flogo Studio design-time handler
//
// A wi-studio contribution (same pattern as the OOTB Invoke REST Service and
// this repo's SOAP Client activity). It runs ONLY in the Flogo designer — it is
// never compiled into or executed by the Go runtime.
//
// Responsibilities:
//   • validate("body") — show the Request Body only for methods that carry one
//     (POST / PUT / PATCH / DELETE), hide it for GET / HEAD / OPTIONS. This
//     mirrors methodAllowsBody() in activity.go so the designer matches runtime.
//   • value("body")    — provide an untyped "{}" object schema by default so the
//     body is always mappable (never "missing" when no schema is defined), while
//     leaving any JSON sample the user has typed untouched.
// =============================================================================
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
exports.RestFireForgetActivityHandler = void 0;

var core_1 = require("@angular/core");
var http_1 = require("@angular/http");
var wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib");

// HTTP methods that carry a request body — keep in sync with methodAllowsBody()
// in activity.go (POST / PUT / PATCH / DELETE).
var BODY_METHODS = { POST: true, PUT: true, PATCH: true, DELETE: true };

var RestFireForgetActivityHandler = function (base) {

    function RestFireForgetHandler(injector, http) {
        var self = base.call(this, injector, http) || this;
        self.injector = injector;
        self.http = http;

        // ── value() ──────────────────────────────────────────────────────────
        // Supplies dynamic field schemas/defaults. Only the body needs one: an
        // untyped "{}" object so it is always mappable, without wiping a JSON
        // sample the user may have typed.
        self.value = function (fieldName, ctx) {
            if (fieldName === "body") {
                var cur = self._get(ctx, "body");
                if (cur) {
                    if (typeof cur === "string") {
                        var t = cur.replace(/^\s+|\s+$/g, "");
                        if (t && t !== "{}") return null; // keep the user's sample/schema
                    } else if (typeof cur === "object") {
                        return null; // already structured — keep it
                    }
                }
                return "{}"; // default: untyped, always-mappable object
            }
            return null;
        };

        // ── validate() ───────────────────────────────────────────────────────
        // Controls field visibility. Show the Request Body only for methods that
        // carry one; hide it for GET / HEAD / OPTIONS.
        self.validate = function (fieldName, ctx) {
            if (fieldName === "body") {
                var method = self._get(ctx, "method");
                var show = !!(method && BODY_METHODS[String(method).toUpperCase()]);
                return wi_contrib_1.ValidationResult.newValidationResult().setVisible(show);
            }
            return null;
        };

        // ── action() ─────────────────────────────────────────────────────────
        self.action = function (actionName, ctx) { return null; };

        return self;
    }

    __extends(RestFireForgetHandler, base);

    // Safely read a field's current value (setting or input).
    RestFireForgetHandler.prototype._get = function (ctx, name) {
        var f = ctx.getField(name);
        return (f && f.value !== undefined) ? f.value : null;
    };

    RestFireForgetHandler = __decorate([
        wi_contrib_1.WiContrib({}),
        core_1.Injectable(),
        __metadata("design:paramtypes", [core_1.Injector, http_1.Http])
    ], RestFireForgetHandler);

    return RestFireForgetHandler;
}(wi_contrib_1.WiServiceHandlerContribution);

exports.RestFireForgetActivityHandler = RestFireForgetActivityHandler;
