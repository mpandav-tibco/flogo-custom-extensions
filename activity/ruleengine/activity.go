package ruleengine

import (
	"fmt"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine"
	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/coerce"
	"github.com/project-flogo/core/support/log"
	"github.com/project-flogo/core/support/trace"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

func init() {
	_ = activity.Register(&Activity{}, New)
}

// ─── Settings ────────────────────────────────────────────────────────────────

// Settings holds design-time configuration for the activity.
type Settings struct {
	// DefaultRulesPath is the fallback rules directory used when the rulesPath
	// input is left empty at runtime. Useful for deploying with a bundled rule
	// set that flows can override on a per-invocation basis.
	DefaultRulesPath string `md:"defaultRulesPath"`
}

// ─── Input ────────────────────────────────────────────────────────────────────

type Input struct {
	Content        string   `md:"content"`
	FileName       string   `md:"fileName"`
	RulesPath      string   `md:"rulesPath"`
	ParserOverride string   `md:"parserOverride"`
	DisabledRules  []string `md:"disabledRules"`
	Tags           []string `md:"tags"`
}

func (i *Input) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"content":        i.Content,
		"fileName":       i.FileName,
		"rulesPath":      i.RulesPath,
		"parserOverride": i.ParserOverride,
		"disabledRules":  i.DisabledRules,
		"tags":           i.Tags,
	}
}

func (i *Input) FromMap(values map[string]interface{}) error {
	var err error
	if i.Content, err = coerce.ToString(values["content"]); err != nil {
		return err
	}
	if i.FileName, err = coerce.ToString(values["fileName"]); err != nil {
		return err
	}
	if i.RulesPath, err = coerce.ToString(values["rulesPath"]); err != nil {
		return err
	}
	if i.ParserOverride, err = coerce.ToString(values["parserOverride"]); err != nil {
		return err
	}
	if v := values["disabledRules"]; v != nil {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				s, err := coerce.ToString(item)
				if err != nil {
					return err
				}
				i.DisabledRules = append(i.DisabledRules, s)
			}
		}
	}
	if v := values["tags"]; v != nil {
		if arr, ok := v.([]interface{}); ok {
			for _, item := range arr {
				s, err := coerce.ToString(item)
				if err != nil {
					return err
				}
				i.Tags = append(i.Tags, s)
			}
		}
	}
	return nil
}

// ─── Output ───────────────────────────────────────────────────────────────────

type Output struct {
	Findings     []interface{}          `md:"findings"`
	Positives    []interface{}          `md:"positives"`
	ErrorCount   int                    `md:"errorCount"`
	WarningCount int                    `md:"warningCount"`
	InfoCount    int                    `md:"infoCount"`
	GoodCount    int                    `md:"goodCount"`
	Markdown     string                 `md:"markdown"`
	Overview     map[string]interface{} `md:"overview"`
	Success      bool                   `md:"success"`
	Error        string                 `md:"error"`
}

func (o *Output) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"findings":     o.Findings,
		"positives":    o.Positives,
		"errorCount":   o.ErrorCount,
		"warningCount": o.WarningCount,
		"infoCount":    o.InfoCount,
		"goodCount":    o.GoodCount,
		"markdown":     o.Markdown,
		"overview":     o.Overview,
		"success":      o.Success,
		"error":        o.Error,
	}
}

func (o *Output) FromMap(values map[string]interface{}) error {
	var err error
	if o.ErrorCount, err = coerce.ToInt(values["errorCount"]); err != nil {
		return err
	}
	if o.WarningCount, err = coerce.ToInt(values["warningCount"]); err != nil {
		return err
	}
	if o.InfoCount, err = coerce.ToInt(values["infoCount"]); err != nil {
		return err
	}
	if o.GoodCount, err = coerce.ToInt(values["goodCount"]); err != nil {
		return err
	}
	if o.Markdown, err = coerce.ToString(values["markdown"]); err != nil {
		return err
	}
	if o.Success, err = coerce.ToBool(values["success"]); err != nil {
		return err
	}
	if o.Error, err = coerce.ToString(values["error"]); err != nil {
		return err
	}
	o.Findings, _ = values["findings"].([]interface{})
	o.Positives, _ = values["positives"].([]interface{})
	o.Overview, _ = values["overview"].(map[string]interface{})
	return nil
}

// ─── Activity ─────────────────────────────────────────────────────────────────

type Activity struct {
	logger           log.Logger
	defaultRulesPath string
}

func New(ctx activity.InitContext) (activity.Activity, error) {
	defaultRulesPath := ""
	if v, ok := ctx.Settings()["defaultRulesPath"]; ok && v != nil {
		defaultRulesPath = fmt.Sprintf("%v", v)
	}
	return &Activity{
		logger:           ctx.Logger(),
		defaultRulesPath: defaultRulesPath,
	}, nil
}

func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

func (a *Activity) Eval(ctx activity.Context) (bool, error) {
	in := &Input{}
	if err := ctx.GetInputObject(in); err != nil {
		return false, err
	}

	// Fall back to the design-time default when rulesPath is not provided at runtime.
	if in.RulesPath == "" && a.defaultRulesPath != "" {
		in.RulesPath = a.defaultRulesPath
	}

	// Both sources are empty: fail fast rather than silently walking the CWD.
	if in.RulesPath == "" {
		return true, ctx.SetOutputObject(&Output{
			Success: false,
			Error:   "rulesPath is required; provide it as an input or configure defaultRulesPath as a setting",
		})
	}

	// OTel tracing: start a child span for this rule evaluation.
	// Uses the Flogo trace abstraction so it delegates to whichever backend
	// (OTel, Jaeger, no-op) is registered at runtime.
	var spanCtx trace.TracingContext
	if trace.Enabled() {
		tracer := trace.GetTracer()
		var spanErr error
		spanCtx, spanErr = tracer.StartTrace(trace.Config{
			Operation: "RuleEngine.Evaluate",
			Tags: map[string]interface{}{
				"ruleengine.file_name":  in.FileName,
				"ruleengine.rules_path": in.RulesPath,
				"ruleengine.tags":       in.Tags,
				"ruleengine.parser":     in.ParserOverride,
			},
			Logger: a.logger,
		}, ctx.GetTracingContext())
		if spanErr != nil {
			a.logger.Warnf("RuleEngine: failed to start trace span: %s", spanErr)
			spanCtx = nil
		}
	}

	result, evalErr := engine.Evaluate(engine.Request{
		Content:        in.Content,
		FileName:       in.FileName,
		RulesPath:      in.RulesPath,
		ParserOverride: in.ParserOverride,
		DisabledRules:  in.DisabledRules,
		Tags:           in.Tags,
	})

	// Finish span after evaluation — record result attributes and any error.
	if spanCtx != nil {
		if evalErr == nil {
			spanCtx.SetTags(map[string]interface{}{
				"ruleengine.rules_run":     result.Overview["rules_run"],
				"ruleengine.parser_used":   result.Overview["parser"],
				"ruleengine.error_count":   result.ErrorCount,
				"ruleengine.warning_count": result.WarningCount,
				"ruleengine.info_count":    result.InfoCount,
			})
		}
		_ = trace.GetTracer().FinishTrace(spanCtx, evalErr)
	}

	out := &Output{}
	if evalErr != nil {
		out.Success = false
		out.Error = evalErr.Error()
		return true, ctx.SetOutputObject(out)
	}

	out.Success = true
	out.Findings = result.FindingsAsInterface()
	out.Positives = result.PositivesAsInterface()
	out.ErrorCount = result.ErrorCount
	out.WarningCount = result.WarningCount
	out.InfoCount = result.InfoCount
	out.GoodCount = result.GoodCount
	out.Markdown = result.Markdown
	out.Overview = result.Overview

	return true, ctx.SetOutputObject(out)
}
