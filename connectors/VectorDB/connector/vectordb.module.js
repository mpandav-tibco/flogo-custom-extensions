"use strict";
var __decorate = this && this.__decorate || function (t, e, i, n) { var r, o = arguments.length, a = o < 3 ? e : null === n ? n = Object.getOwnPropertyDescriptor(e, i) : n; if ("object" == typeof Reflect && "function" == typeof Reflect.decorate) a = Reflect.decorate(t, e, i, n); else for (var s = t.length - 1; s >= 0; s--)(r = t[s]) && (a = (o < 3 ? r(a) : o > 3 ? r(e, i, a) : r(e, i)) || a); return o > 3 && a && Object.defineProperty(e, i, a), a };
Object.defineProperty(exports, "__esModule", { value: !0 });
var wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib"),
    core_1 = require("@angular/core"),
    common_1 = require("@angular/common"),
    http_1 = require("@angular/http"),
    vectordbHandler_1 = require("./vectordbHandler"),
    vectordbModule = function () {
        function e() { }
        e = __decorate([core_1.NgModule({
            imports: [common_1.CommonModule, http_1.HttpModule],
            exports: [],
            declarations: [],
            entryComponents: [],
            providers: [{ provide: wi_contrib_1.WiServiceContribution, useClass: vectordbHandler_1.vectordbHandler }],
            bootstrap: []
        })], e);
        return e;
    }();
exports.default = vectordbModule;
