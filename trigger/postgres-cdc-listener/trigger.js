"use strict";
var __extends = this && this.__extends || function () {
    var e = function (t, i) {
        return (e = Object.setPrototypeOf || { __proto__: [] } instanceof Array && function (e, t) { e.__proto__ = t; } || function (e, t) { for (var i in t) Object.prototype.hasOwnProperty.call(t, i) && (e[i] = t[i]); })(t, i);
    };
    return function (t, i) {
        if ("function" != typeof i && null !== i) throw new TypeError("Class extends value " + String(i) + " is not a constructor or null");
        function a() { this.constructor = t; }
        e(t, i), t.prototype = null === i ? Object.create(i) : (a.prototype = i.prototype, new a());
    };
}();
var __decorate = this && this.__decorate || function (e, t, i, a) {
    var n, r = arguments.length, s = r < 3 ? t : null === a ? a = Object.getOwnPropertyDescriptor(t, i) : a;
    if ("object" == typeof Reflect && "function" == typeof Reflect.decorate) s = Reflect.decorate(e, t, i, a);
    else for (var o = e.length - 1; o >= 0; o--) (n = e[o]) && (s = (r < 3 ? n(s) : r > 3 ? n(t, i, s) : n(t, i)) || s);
    return r > 3 && s && Object.defineProperty(t, i, s), s;
};
var __metadata = this && this.__metadata || function (e, t) {
    if ("object" == typeof Reflect && "function" == typeof Reflect.metadata) return Reflect.metadata(e, t);
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.PostgresCDCListenerHandler = void 0;

var core_1 = require("@angular/core");
var http_1 = require("@angular/http");
var wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib");
var validation_1 = require("wi-studio/common/models/validation");

var PostgresCDCListenerHandler = function (e) {
    function t(t, i, a) {
        var n = e.call(this, t, i, a) || this;
        n.injector = t;
        n.http = i;
        n.contribModelService = a;

        n.value = function (fieldName, settings) {
            return null;
        };

        // TLS field visibility:
        //   Level 1 — tlsConfig boolean: hides sslMode, sslCA, sslCert, sslKey when off.
        //   Level 2 — sslMode dropdown: sslCA only for verify-ca/verify-full; sslCert/sslKey
        //             available whenever TLS is enabled (optional client authentication).
        n.validate = function (fieldName, settings) {
            var tlsField = settings.getField("tlsConfig");
            var tlsEnabled = tlsField && (tlsField.value === true || tlsField.value === "true");

            switch (fieldName) {
                case "sslMode":
                    return validation_1.ValidationResult.newValidationResult().setVisible(!!tlsEnabled);

                case "sslCA": {
                    if (!tlsEnabled) return validation_1.ValidationResult.newValidationResult().setVisible(false);
                    var sslMode = settings.getField("sslMode");
                    var mode = sslMode ? sslMode.value : "require";
                    return validation_1.ValidationResult.newValidationResult()
                        .setVisible(mode === "verify-ca" || mode === "verify-full");
                }
                case "sslCert":
                case "sslKey":
                    return validation_1.ValidationResult.newValidationResult().setVisible(!!tlsEnabled);

                default:
                    return null;
            }
        };

        n.action = function (actionName, settings) {
            return null;
        };

        return n;
    }

    __extends(t, e);
    t = __decorate([
        wi_contrib_1.WiContrib({}),
        core_1.Injectable(),
        __metadata("design:paramtypes", [core_1.Injector, http_1.Http, wi_contrib_1.WiContribModelService])
    ], t);
    return t;
}(wi_contrib_1.WiServiceHandlerContribution);

exports.PostgresCDCListenerHandler = PostgresCDCListenerHandler;
