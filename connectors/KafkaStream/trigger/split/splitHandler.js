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
exports.splitHandler = void 0;

var core_1 = require("@angular/core");
var http_1 = require("@angular/http");
var Observable_1 = require("rxjs/Observable");
var lodash = require("lodash");
var wi_contrib_1 = require("wi-studio/app/contrib/wi-contrib");

var splitHandler = function (e) {
    function t(t, i, a) {
        var n = e.call(this, t, i, a) || this;
        n.injector = t;
        n.http = i;
        n.contribModelService = a;

        // Populate the kafkaConnection dropdown with available TIBCO Kafka connections.
        n.value = function (e, t) {
            if ("kafkaConnection" === e) {
                return Observable_1.Observable.create(function (observer) {
                    var connections = [];
                    wi_contrib_1.WiContributionUtils.getConnections(n.http, "Kafka", "tibco-kafka").subscribe(function (conns) {
                        conns.forEach(function (conn) {
                            for (var unique_id, name, l = 0; l < conn.settings.length; l++) {
                                if ("name" === conn.settings[l].name) {
                                    unique_id = wi_contrib_1.WiContributionUtils.getUniqueId(conn);
                                    name = conn.settings[l].value;
                                    connections.push({ unique_id: unique_id, name: name });
                                    break;
                                }
                            }
                        });
                        observer.next(connections);
                    });
                });
            }
            return null;
        };

        // Show/hide handler-level predicate fields based on the selected eventType.
        // - matched:   show all predicate fields (field, operator, value, predicates, predicateMode, priority)
        // - unmatched: hide predicate fields; show only priority
        // - evalError: hide predicate fields; show only priority
        // - all:       hide predicate fields; show only priority
        n.validate = function (fieldName, context) {
            var result = wi_contrib_1.ValidationResult.newValidationResult();
            var eventType = context.getField("eventType");
            var isMatched = !eventType || !eventType.value || eventType.value === "matched";

            if (fieldName === "field" || fieldName === "operator" || fieldName === "value") {
                result.setVisible(isMatched);
            }
            if (fieldName === "predicates" || fieldName === "predicateMode") {
                result.setVisible(isMatched);
            }
            if (fieldName === "priority") {
                // Priority is always visible — it controls evaluation order for first-match.
                result.setVisible(true);
            }
            return result;
        };

        n.action = function (e, t) {
            var i = n.getModelService();
            var a = wi_contrib_1.CreateFlowActionResult.newActionResult();
            var r = t.getField("kafkaConnection");
            if (r && r.value) {
                var s = i.createTriggerElement("KafkaStream/kafka-stream-split-trigger");
                if (s && s.settings) {
                    var o = s.getField("kafkaConnection");
                    o.value = r.value;
                    o.allowed = r.allowed;
                    var l = i.createFlow(t.getFlowName(), t.getFlowDescription());
                    a = a.addTriggerFlowMapping(lodash.cloneDeep(s), lodash.cloneDeep(l));
                }
            }
            return wi_contrib_1.ActionResult.newActionResult().setSuccess(true).setResult(a);
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

exports.splitHandler = splitHandler;
