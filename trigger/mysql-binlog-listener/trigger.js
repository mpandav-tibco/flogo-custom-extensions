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
exports.MySQLBinlogListenerHandler = void 0;

var core_1 = require("@angular/core");
var http_1 = require("@angular/http");
var wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib");
var validation_1 = require("wi-studio/common/models/validation");

var MySQLBinlogListenerHandler = function (e) {
    function t(t, i, a) {
        var n = e.call(this, t, i, a) || this;
        n.injector = t;
        n.http = i;
        n.contribModelService = a;

        n.value = function (fieldName, settings) {
            return null;
        };

        // Two-level TLS visibility logic:
        //
        // Level 1 — tlsConfig boolean (the on/off toggle):
        //   sslMode, sslCA, sslCert, sslKey, skipSSLVerify are ALL hidden when tlsConfig=false
        //
        // Level 2 — sslMode dropdown (only meaningful when tlsConfig=true):
        //   sslCA         — only for verify-ca and verify-full
        //   sslCert/sslKey — only for require(+skipSSL), verify-ca, verify-full
        //   skipSSLVerify  — only for require mode
        //
        // Heartbeat:
        //   heartbeatInterval — only when enableHeartbeat=true
        //
        // Handler:
        //   binlogPos — only when binlogFile is non-empty
        n.validate = function (fieldName, settings) {
            var tlsField = settings.getField("tlsConfig");
            var tlsEnabled = tlsField && (tlsField.value === true || tlsField.value === "true");

            switch (fieldName) {
                case "sslMode":
                    return validation_1.ValidationResult.newValidationResult()
                        .setVisible(!!tlsEnabled);

                case "sslCA": {
                    if (!tlsEnabled) return validation_1.ValidationResult.newValidationResult().setVisible(false);
                    var sslMode = settings.getField("sslMode");
                    var mode = sslMode ? sslMode.value : "require";
                    return validation_1.ValidationResult.newValidationResult()
                        .setVisible(mode === "verify-ca" || mode === "verify-full");
                }
                case "sslCert":
                case "sslKey": {
                    if (!tlsEnabled) return validation_1.ValidationResult.newValidationResult().setVisible(false);
                    return validation_1.ValidationResult.newValidationResult().setVisible(true);
                }
                case "skipSSLVerify": {
                    if (!tlsEnabled) return validation_1.ValidationResult.newValidationResult().setVisible(false);
                    var sslMode = settings.getField("sslMode");
                    var mode = sslMode ? sslMode.value : "require";
                    return validation_1.ValidationResult.newValidationResult()
                        .setVisible(mode === "require");
                }
                case "heartbeatInterval": {
                    var hbField = settings.getField("enableHeartbeat");
                    var enabled = hbField && (hbField.value === true || hbField.value === "true");
                    return validation_1.ValidationResult.newValidationResult()
                        .setVisible(!!enabled);
                }
                case "binlogPos": {
                    var bf = settings.getField("binlogFile");
                    var hasFile = bf && bf.value != null && bf.value !== "";
                    return validation_1.ValidationResult.newValidationResult()
                        .setVisible(!!hasFile);
                }
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

exports.MySQLBinlogListenerHandler = MySQLBinlogListenerHandler;
