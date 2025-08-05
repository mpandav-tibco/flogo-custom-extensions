package templateengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/project-flogo/core/activity"
	"github.com/project-flogo/core/data/metadata"
	"github.com/project-flogo/core/support/log"
)

var activityMd = activity.ToMetadata(&Settings{}, &Input{}, &Output{})

// Compiled regex for performance
var numberedListRegex = regexp.MustCompile(`^\d+\.`)

func init() {
	_ = activity.Register(&Activity{}, New)
}

type Settings struct {
	TemplateEngine    string `md:"templateEngine"`
	TemplateCacheSize int    `md:"templateCacheSize"`
	EnableSafeMode    bool   `md:"enableSafeMode"`
	TemplatePath      string `md:"templatePath"`
}

type Input struct {
	TemplateType      string                 `md:"templateType"`
	Template          string                 `md:"template"`
	TemplateData      map[string]interface{} `md:"templateData"`
	OutputFormat      string                 `md:"outputFormat"`
	EnableFormatting  bool                   `md:"enableFormatting"`
	TemplateVariables map[string]interface{} `md:"templateVariables"`
	EscapeHtml        bool                   `md:"escapeHtml"`
	StrictMode        bool                   `md:"strictMode"`
}

type Output struct {
	Result         string   `md:"result"`
	Success        bool     `md:"success"`
	Error          string   `md:"error"`
	TemplateUsed   string   `md:"templateUsed"`
	ProcessingTime int64    `md:"processingTime"`
	VariablesUsed  []string `md:"variablesUsed"`
}

// Activity is the template engine activity
type Activity struct {
	settings            *Settings
	logger              log.Logger
	templateCache       sync.Map
	templateBasePath    string
	currentTemplateType string // Track the current template being processed
}

// safeLog safely logs messages when logger is available
func (a *Activity) safeLog(level string, template string, args ...interface{}) {
	if a.logger == nil {
		return
	}
	switch level {
	case "info":
		a.logger.Infof(template, args...)
	case "debug":
		a.logger.Debugf(template, args...)
	case "error":
		a.logger.Errorf(template, args...)
	case "warn":
		a.logger.Warnf(template, args...)
	}
}

// New creates a new template engine activity
func New(ctx activity.InitContext) (activity.Activity, error) {
	s := &Settings{}
	err := metadata.MapToStruct(ctx.Settings(), s, true)
	if err != nil {
		return nil, err
	}

	logger := ctx.Logger()

	// Determine template base path
	templateBasePath := GetTemplateBasePath(s.TemplatePath)

	act := &Activity{
		settings:            s,
		logger:              logger,
		templateBasePath:    templateBasePath,
		currentTemplateType: "", // Initialize empty, will be set during processing
	}

	if logger != nil {
		logger.Infof("Template Engine activity initialized with template path: %s", templateBasePath)
	}
	return act, nil
}

// GetTemplateBasePath determines the base path for templates
func GetTemplateBasePath(configuredPath string) string {
	// If a custom path is configured, use it
	if configuredPath != "" {
		if filepath.IsAbs(configuredPath) {
			return configuredPath
		}
		// If relative path, make it relative to current working directory
		if wd, err := os.Getwd(); err == nil {
			return filepath.Join(wd, configuredPath)
		}
		return configuredPath
	}

	// Strategy 1: Try to detect using runtime caller
	for i := 0; i < 10; i++ {
		_, currentFile, _, ok := runtime.Caller(i)
		if ok && strings.Contains(currentFile, "templateengine") {
			sourceDir := filepath.Dir(currentFile)
			templatesPath := filepath.Join(sourceDir, "templates")
			if _, err := os.Stat(templatesPath); err == nil {
				return templatesPath
			}
		}
	}

	// Strategy 2: Look for GOPATH or GOMOD based paths
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		possiblePaths := []string{
			filepath.Join(gopath, "src", "github.com", "milindpandav", "activity", "templateengine", "templates"),
			filepath.Join(gopath, "pkg", "mod", "github.com", "milindpandav", "activity", "templateengine*", "templates"),
		}
		for _, path := range possiblePaths {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}

	// Strategy 3: Search in common locations
	fallbackPaths := []string{
		"templates",            // Current working directory
		"./templates",          // Explicit relative path
		"../templates",         // Parent directory
		"../../templates",      // Grandparent directory
		"../../../templates",   // Great grandparent directory
		"/tmp/flogo-templates", // System temp directory
		"./custom-extensions-1/activity/templateengine/templates", // Development path
		"../custom-extensions-1/activity/templateengine/templates",
		"../../custom-extensions-1/activity/templateengine/templates",
	}

	for _, path := range fallbackPaths {
		absPath, err := filepath.Abs(path)
		if err == nil {
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
	}

	// Strategy 4: Try to find in executable directory
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		templatesPath := filepath.Join(execDir, "templates")
		if _, err := os.Stat(templatesPath); err == nil {
			return templatesPath
		}
	}

	// Default fallback
	return "templates"
}

// Metadata returns the activity's metadata
func (a *Activity) Metadata() *activity.Metadata {
	return activityMd
}

// Eval implements activity.Activity.Eval
func (a *Activity) Eval(ctx activity.Context) (done bool, err error) {
	startTime := time.Now()

	// Get tracing context for OTEL support
	tracingCtx := ctx.GetTracingContext()
	if tracingCtx != nil {
		// Set initial span tags for observability
		tracingCtx.SetTag("activity.name", "template-engine")
		tracingCtx.SetTag("activity.type", "transformation")
	}

	// Get inputs using individual field access
	templateType, _ := ctx.GetInput("templateType").(string)
	template, _ := ctx.GetInput("template").(string)
	templateData, _ := ctx.GetInput("templateData").(map[string]interface{})
	outputFormat, _ := ctx.GetInput("outputFormat").(string)
	enableFormatting, _ := ctx.GetInput("enableFormatting").(bool)
	templateVariables, _ := ctx.GetInput("templateVariables").(map[string]interface{})
	escapeHtml, _ := ctx.GetInput("escapeHtml").(bool)
	strictMode, _ := ctx.GetInput("strictMode").(bool)

	// Add trace tags for observability
	if tracingCtx != nil {
		tracingCtx.SetTags(map[string]interface{}{
			"template.type":              templateType,
			"template.output_format":     outputFormat,
			"template.enable_formatting": enableFormatting,
			"template.escape_html":       escapeHtml,
			"template.strict_mode":       strictMode,
			"template.safe_mode":         a.settings.EnableSafeMode,
			"template.engine":            a.settings.TemplateEngine,
		})

		if templateData != nil {
			tracingCtx.SetTag("template.data_variables_count", len(templateData))
		}
		if templateVariables != nil {
			tracingCtx.SetTag("template.additional_variables_count", len(templateVariables))
		}
	}

	output := &Output{
		Success: false,
	}

	defer func() {
		output.ProcessingTime = time.Since(startTime).Nanoseconds() / 1000000 // Convert to milliseconds

		// Add processing metrics to trace
		if tracingCtx != nil {
			metricsTags := map[string]interface{}{
				"template.processing_time_ms": output.ProcessingTime,
				"template.success":            output.Success,
			}

			if output.Result != "" {
				metricsTags["template.result_length"] = len(output.Result)
			}
			if len(output.VariablesUsed) > 0 {
				metricsTags["template.variables_used_count"] = len(output.VariablesUsed)
			}
			if output.Error != "" {
				metricsTags["template.error"] = output.Error
				// Log error details for observability
				tracingCtx.LogKV(map[string]interface{}{
					"error.type":    "template_processing_error",
					"error.message": output.Error,
					"error.stage":   "template_execution",
				})
			}

			tracingCtx.SetTags(metricsTags)
		}

		// Set individual outputs instead of using SetOutputObject
		ctx.SetOutput("result", output.Result)
		ctx.SetOutput("success", output.Success)
		ctx.SetOutput("error", output.Error)
		ctx.SetOutput("templateUsed", output.TemplateUsed)
		ctx.SetOutput("processingTime", output.ProcessingTime)
		ctx.SetOutput("variablesUsed", output.VariablesUsed)

		// Log completion with processing time
		if output.Success {
			a.safeLog("info", "Template processing completed successfully in %dms",
				output.ProcessingTime)
		}
	}()

	// Determine which template to use
	templateContent, err := a.getTemplate(templateType, template)
	if err != nil {
		output.Error = fmt.Sprintf("Failed to get template: %v", err)
		a.safeLog("error", output.Error)
		return true, nil
	}

	output.TemplateUsed = templateContent

	// Log template selection
	if templateType != "" && templateType != "custom" {
		a.safeLog("debug", "Using OOTB template: '%s' (%d characters)", templateType, len(templateContent))
	} else {
		a.safeLog("debug", "Using custom template (%d characters)", len(templateContent))
	}

	// Merge template data with additional variables
	mergedData := make(map[string]interface{})
	for k, v := range templateData {
		mergedData[k] = v
	}
	for k, v := range templateVariables {
		mergedData[k] = v
	}

	// Add system variables
	mergedData["_timestamp"] = time.Now().Format(time.RFC3339)
	mergedData["_date"] = time.Now().Format("2006-01-02")
	mergedData["_time"] = time.Now().Format("15:04:05")
	mergedData["_year"] = time.Now().Year()

	a.safeLog("debug", "Template data merged - Total variables: %d", len(mergedData))

	// Process the template
	result, variablesUsed, err := a.processTemplate(templateContent, mergedData, strictMode)
	if err != nil {
		output.Error = fmt.Sprintf("Template processing failed: %v", err)
		a.safeLog("error", output.Error)
		return true, nil
	}

	// Apply HTML escaping if requested
	if escapeHtml {
		result = html.EscapeString(result)
		a.safeLog("debug", "HTML escaping applied to output")
	}

	// Apply formatting if enabled
	if enableFormatting {
		result, err = a.formatOutput(result, outputFormat)
		if err != nil {
			a.safeLog("warn", "Output formatting failed: %v", err)
		} else {
			a.safeLog("debug", "Output formatted as %s", outputFormat)
		}
	}

	output.Result = result
	output.Success = true
	output.VariablesUsed = variablesUsed

	return true, nil
}

// getTemplate returns the template content based on type or custom template
func (a *Activity) getTemplate(templateType, customTemplate string) (string, error) {
	// Track current template type for metadata
	if templateType == "" || templateType == "custom" {
		a.currentTemplateType = "custom"
		if customTemplate == "" {
			return "", fmt.Errorf("no template provided")
		}
		a.safeLog("debug", "Using provided custom template")
		return customTemplate, nil
	}

	// Set current template type for OOTB templates
	a.currentTemplateType = templateType

	// Load OOTB template from file system using detected base path
	templatePath := filepath.Join(a.templateBasePath, templateType+".tmpl")

	a.safeLog("debug", "Loading OOTB template '%s' from path: %s", templateType, a.templateBasePath)
	a.safeLog("debug", "Full template path: %s", templatePath)

	// Check if template file exists
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		// Try fallback: direct filename in base path (in case .tmpl extension is already included)
		alternativePath := filepath.Join(a.templateBasePath, templateType)
		if _, err := os.Stat(alternativePath); os.IsNotExist(err) {
			a.safeLog("error", "Template file not found: %s (also tried: %s)", templatePath, alternativePath)
			return "", fmt.Errorf("template file not found: %s (also tried: %s)", templatePath, alternativePath)
		}
		templatePath = alternativePath
		a.safeLog("debug", "Using alternative path: %s", templatePath)
	}

	// Read template content from file
	content, err := ioutil.ReadFile(templatePath)
	if err != nil {
		a.safeLog("error", "Failed to read template file %s: %v", templatePath, err)
		return "", fmt.Errorf("failed to read template file %s: %v", templatePath, err)
	}

	a.safeLog("info", "Loaded template '%s' from: %s", templateType, templatePath)
	a.safeLog("debug", "Template content: %d bytes", len(content))
	return string(content), nil
}

// processTemplate processes the template using the specified engine
func (a *Activity) processTemplate(templateContent string, data map[string]interface{}, strictMode bool) (string, []string, error) {
	switch a.settings.TemplateEngine {
	case "handlebars", "handlebars-basic", "mustache", "mustache-basic":
		return a.processHandlebarsTemplate(templateContent, data, strictMode)
	default:
		return a.processGoTemplate(templateContent, data, strictMode)
	}
}

// processGoTemplate processes templates using Go's text/template
func (a *Activity) processGoTemplate(templateContent string, data map[string]interface{}, strictMode bool) (string, []string, error) {
	// Generate cache key from template content
	cacheKey := fmt.Sprintf("%x", templateContent)

	// Try to get cached template
	var parsedTemplate *template.Template
	if cached, ok := a.templateCache.Load(cacheKey); ok {
		parsedTemplate = cached.(*template.Template)
		a.safeLog("debug", "Using cached compiled template")
	} else {
		// Create new template with custom functions
		tmpl := template.New("main")

		if !a.settings.EnableSafeMode {
			// Full function set when safe mode is disabled
			a.safeLog("debug", "Loading full template function set (safe mode disabled)")
			tmpl = tmpl.Funcs(a.getTemplateFunctions())
		} else {
			// Essential functions even in safe mode for OOTB templates
			a.safeLog("debug", "Loading essential template functions (safe mode enabled)")
			tmpl = tmpl.Funcs(a.getEssentialTemplateFunctions())
		}

		// Parse template
		a.safeLog("debug", "Compiling template (%d characters)", len(templateContent))
		var err error
		parsedTemplate, err = tmpl.Parse(templateContent)
		if err != nil {
			a.safeLog("error", "Template compilation failed: %v", err)
			return "", nil, fmt.Errorf("template parsing failed: %v", err)
		}

		a.safeLog("debug", "Template compiled successfully")
		// Cache the parsed template if cache size allows
		a.cacheTemplate(cacheKey, parsedTemplate)
	}

	// Execute template with strict mode handling
	var buf bytes.Buffer
	if strictMode {
		// In strict mode, use missingkey=error option
		a.safeLog("debug", "Executing template in strict mode (will fail on undefined variables)")
		parsedTemplate = parsedTemplate.Option("missingkey=error")
	} else {
		// In non-strict mode, use missingkey=zero (default)
		a.safeLog("debug", "Executing template in permissive mode")
		parsedTemplate = parsedTemplate.Option("missingkey=zero")
	}

	a.safeLog("debug", "Executing template with %d data variables", len(data))
	err := parsedTemplate.Execute(&buf, data)
	if err != nil {
		if strictMode {
			a.safeLog("error", "Strict mode execution failed: %v", err)
			return "", nil, fmt.Errorf("strict mode: template execution failed due to missing variables: %v", err)
		}
		a.safeLog("error", "Template execution failed: %v", err)
		return "", nil, fmt.Errorf("template execution failed: %v", err)
	}

	// Extract variables used
	variablesUsed := a.extractVariablesFromTemplate(templateContent)

	resultLength := buf.Len()
	a.safeLog("debug", "Template execution completed - Generated %d characters, Variables detected: %d",
		resultLength, len(variablesUsed))

	return buf.String(), variablesUsed, nil
}

// cacheTemplate stores a compiled template in the cache
func (a *Activity) cacheTemplate(key string, tmpl *template.Template) {
	// Count current cache size
	cacheSize := 0
	a.templateCache.Range(func(_, _ interface{}) bool {
		cacheSize++
		return true
	})

	// If cache is full, remove oldest entry (simple FIFO)
	if cacheSize >= a.settings.TemplateCacheSize {
		a.templateCache.Range(func(k, _ interface{}) bool {
			a.templateCache.Delete(k)
			return false // Stop after deleting one
		})
	}

	// Store in cache
	a.templateCache.Store(key, tmpl)
	a.safeLog("debug", "Template compiled and cached (cache size: %d/%d)", cacheSize+1, a.settings.TemplateCacheSize)
}

// getEssentialTemplateFunctions returns essential functions that are safe and needed for OOTB templates
func (a *Activity) getEssentialTemplateFunctions() template.FuncMap {
	return template.FuncMap{
		// Essential string functions
		"upper":     strings.ToUpper,
		"lower":     strings.ToLower,
		"title":     strings.Title,
		"trim":      strings.TrimSpace,
		"trimSpace": strings.TrimSpace, // Explicit trimSpace function

		// Essential utility functions
		"default": func(defaultVal interface{}, val interface{}) interface{} {
			if val == nil || val == "" {
				return defaultVal
			}
			return val
		},
		"json": func(v interface{}) string {
			bytes, _ := json.Marshal(v)
			return string(bytes)
		},

		// Essential time functions
		"now": time.Now,
		"formatDate": func(layout string, t time.Time) string {
			return t.Format(layout)
		},

		// Essential conditional functions
		"eq": func(a, b interface{}) bool {
			return reflect.DeepEqual(a, b)
		},
		"ne": func(a, b interface{}) bool {
			return !reflect.DeepEqual(a, b)
		},

		// Essential array functions
		"length": func(slice interface{}) int {
			v := reflect.ValueOf(slice)
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array || v.Kind() == reflect.String {
				return v.Len()
			}
			return 0
		},
	}
}

// getTemplateFunctions returns all template functions for full mode
func (a *Activity) getTemplateFunctions() template.FuncMap {
	return template.FuncMap{
		// String functions
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"title": strings.Title,
		"capitalize": func(s string) string {
			if len(s) == 0 {
				return s
			}
			return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
		},
		"truncate": func(length int, s string) string {
			if len(s) <= length {
				return s
			}
			return s[:length] + "..."
		},
		"reverse": func(s string) string {
			runes := []rune(s)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return string(runes)
		},
		"replace": func(old, new, s string) string {
			return strings.ReplaceAll(s, old, new)
		},
		"contains": func(s, substr string) bool {
			return strings.Contains(s, substr)
		},
		"trim": strings.TrimSpace,

		// Math functions
		"add":      func(a, b int) int { return a + b },
		"subtract": func(a, b int) int { return a - b },
		"multiply": func(a, b int) int { return a * b },
		"divide": func(a, b int) int {
			if b != 0 {
				return a / b
			}
			return 0
		},

		// Array functions
		"first": func(slice interface{}) interface{} {
			v := reflect.ValueOf(slice)
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
				if v.Len() > 0 {
					return v.Index(0).Interface()
				}
			}
			return nil
		},
		"last": func(slice interface{}) interface{} {
			v := reflect.ValueOf(slice)
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
				if v.Len() > 0 {
					return v.Index(v.Len() - 1).Interface()
				}
			}
			return nil
		},
		"length": func(slice interface{}) int {
			v := reflect.ValueOf(slice)
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array || v.Kind() == reflect.String {
				return v.Len()
			}
			return 0
		},
		"sort": func(slice interface{}) interface{} {
			v := reflect.ValueOf(slice)
			if v.Kind() == reflect.Slice {
				// Simple string sort for demonstration
				if v.Type().Elem().Kind() == reflect.String {
					strings := make([]string, v.Len())
					for i := 0; i < v.Len(); i++ {
						strings[i] = v.Index(i).String()
					}
					sort.Strings(strings)
					return strings
				}
			}
			return slice
		},
		"join": func(slice interface{}, separator string) string {
			v := reflect.ValueOf(slice)
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
				strs := make([]string, v.Len())
				for i := 0; i < v.Len(); i++ {
					strs[i] = fmt.Sprintf("%v", v.Index(i).Interface())
				}
				return strings.Join(strs, separator)
			}
			return ""
		},
		"split": func(s, separator string) []string {
			return strings.Split(s, separator)
		},

		// Conditional functions
		"eq": func(a, b interface{}) bool { return a == b },
		"ne": func(a, b interface{}) bool { return a != b },
		"lt": func(a, b interface{}) bool {
			return compareValues(a, b) < 0
		},
		"gt": func(a, b interface{}) bool {
			return compareValues(a, b) > 0
		},
		"le": func(a, b interface{}) bool {
			return compareValues(a, b) <= 0
		},
		"ge": func(a, b interface{}) bool {
			return compareValues(a, b) >= 0
		},

		// Utility functions
		"default": func(defaultValue interface{}, value interface{}) interface{} {
			if value == nil || value == "" {
				return defaultValue
			}
			return value
		},
		"json": func(v interface{}) string {
			bytes, err := json.Marshal(v)
			if err != nil {
				return fmt.Sprintf("json error: %v", err)
			}
			return string(bytes)
		},
		"formatDate": func(format string, date interface{}) string {
			switch d := date.(type) {
			case time.Time:
				return d.Format(format)
			case string:
				if t, err := time.Parse(time.RFC3339, d); err == nil {
					return t.Format(format)
				}
			}
			return ""
		},
		"now": time.Now,
	}
}

// compareValues compares two values for ordering
func compareValues(a, b interface{}) int {
	switch av := a.(type) {
	case int:
		if bv, ok := b.(int); ok {
			if av < bv {
				return -1
			} else if av > bv {
				return 1
			}
			return 0
		}
	case string:
		if bv, ok := b.(string); ok {
			return strings.Compare(av, bv)
		}
	}
	return 0
}

// extractVariablesFromTemplate extracts variable names from template content
func (a *Activity) extractVariablesFromTemplate(content string) []string {
	// Simple regex to find template variables
	re := regexp.MustCompile(`\{\{\.([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)
	matches := re.FindAllStringSubmatch(content, -1)

	variables := make([]string, 0, len(matches))
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) > 1 {
			variable := match[1]
			if !seen[variable] {
				variables = append(variables, variable)
				seen[variable] = true
			}
		}
	}

	return variables
}

// formatOutput formats the output based on the specified format
func (a *Activity) formatOutput(content, format string) (string, error) {
	switch format {
	case "json":
		// For JSON format, wrap content in a JSON object with rich metadata
		var obj interface{}
		if err := json.Unmarshal([]byte(content), &obj); err == nil {
			// Content is already valid JSON, wrap it with metadata
			safeMode := false
			if a.settings != nil {
				safeMode = a.settings.EnableSafeMode
			}
			jsonContent := map[string]interface{}{
				"content":          obj,
				"contentType":      "application/json",
				"format":           "json",
				"templateType":     a.getCurrentTemplateType(),
				"processingEngine": "go-template",
				"safeMode":         safeMode,
				"timestamp":        time.Now().Format(time.RFC3339),
				"metadata": map[string]interface{}{
					"isPreformattedJSON": true,
					"originalFormat":     "json",
					"contentLength":      len(content),
				},
			}
			formatted, err := json.MarshalIndent(jsonContent, "", "  ")
			if err == nil {
				return string(formatted), nil
			}
		}
		// Content is plain text, wrap it in JSON structure with rich metadata
		contentType := a.detectContentType(content)
		safeMode := false
		if a.settings != nil {
			safeMode = a.settings.EnableSafeMode
		}
		jsonContent := map[string]interface{}{
			"content":          content,
			"contentType":      contentType,
			"format":           "text",
			"templateType":     a.getCurrentTemplateType(),
			"processingEngine": "go-template",
			"safeMode":         safeMode,
			"timestamp":        time.Now().Format(time.RFC3339),
			"metadata": map[string]interface{}{
				"isPreformattedJSON": false,
				"originalFormat":     "text",
				"contentLength":      len(content),
				"lineCount":          len(strings.Split(content, "\n")),
				"hasSubject":         strings.Contains(content, "Subject:"),
				"hasListItems":       strings.Contains(content, "•") || strings.Contains(content, "*") || regexp.MustCompile(`^\d+\.`).MatchString(content),
				"detectedStructure":  a.analyzeContentStructure(content),
			},
		}
		formatted, err := json.MarshalIndent(jsonContent, "", "  ")
		if err != nil {
			return content, err
		}
		return string(formatted), nil
	case "xml":
		// Convert text to XML format
		return a.formatAsXML(content), nil
	case "html":
		// Convert text to HTML format
		return a.formatAsHTML(content), nil
	case "markdown":
		// Convert text to Markdown format
		return a.formatAsMarkdown(content), nil
	default:
		return content, nil
	}
}

// formatAsHTML converts plain text content to HTML format
func (a *Activity) formatAsHTML(content string) string {
	var htmlBuilder strings.Builder

	htmlBuilder.WriteString("<!DOCTYPE html>\n<html>\n<head>\n")
	htmlBuilder.WriteString("    <meta charset=\"UTF-8\">\n")
	htmlBuilder.WriteString("    <title>Template Output</title>\n")
	htmlBuilder.WriteString("    <style>\n")
	htmlBuilder.WriteString("        body { font-family: Arial, sans-serif; line-height: 1.6; margin: 40px; }\n")
	htmlBuilder.WriteString("        h1, h2, h3 { color: #333; }\n")
	htmlBuilder.WriteString("        ul { margin: 10px 0; }\n")
	htmlBuilder.WriteString("        li { margin: 5px 0; }\n")
	htmlBuilder.WriteString("        .email-subject { font-weight: bold; font-size: 1.2em; margin-bottom: 20px; }\n")
	htmlBuilder.WriteString("        .section { margin: 20px 0; }\n")
	htmlBuilder.WriteString("    </style>\n")
	htmlBuilder.WriteString("</head>\n<body>\n")

	lines := strings.Split(content, "\n")
	inList := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			if inList {
				htmlBuilder.WriteString("</ul>\n")
				inList = false
			}
			htmlBuilder.WriteString("<br>\n")
			continue
		}

		// Check if it's a subject line
		if strings.HasPrefix(line, "Subject:") {
			htmlBuilder.WriteString("    <div class=\"email-subject\">")
			htmlBuilder.WriteString(html.EscapeString(line))
			htmlBuilder.WriteString("</div>\n")
			continue
		}

		// Check if it's a list item (starts with bullet or number)
		if strings.HasPrefix(line, "•") || strings.HasPrefix(line, "*") || regexp.MustCompile(`^\d+\.`).MatchString(line) {
			if !inList {
				htmlBuilder.WriteString("    <ul>\n")
				inList = true
			}
			// Remove bullet/number and convert to list item
			listContent := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "•"), "*"))
			if regexp.MustCompile(`^\d+\.`).MatchString(line) {
				listContent = regexp.MustCompile(`^\d+\.\s*`).ReplaceAllString(line, "")
			}
			htmlBuilder.WriteString("        <li>")
			htmlBuilder.WriteString(html.EscapeString(listContent))
			htmlBuilder.WriteString("</li>\n")
			continue
		}

		// Regular paragraph
		if inList {
			htmlBuilder.WriteString("    </ul>\n")
			inList = false
		}

		htmlBuilder.WriteString("    <p>")
		htmlBuilder.WriteString(html.EscapeString(line))
		htmlBuilder.WriteString("</p>\n")
	}

	if inList {
		htmlBuilder.WriteString("    </ul>\n")
	}

	htmlBuilder.WriteString("</body>\n</html>")
	return htmlBuilder.String()
}

// formatAsXML converts plain text content to XML format
func (a *Activity) formatAsXML(content string) string {
	var xml strings.Builder

	xml.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	xml.WriteString("<document>\n")

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "Subject:") {
			xml.WriteString("    <subject>")
			xml.WriteString(html.EscapeString(strings.TrimPrefix(line, "Subject:")))
			xml.WriteString("</subject>\n")
		} else if strings.HasPrefix(line, "•") || strings.HasPrefix(line, "*") {
			listContent := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "•"), "*"))
			xml.WriteString("    <item>")
			xml.WriteString(html.EscapeString(listContent))
			xml.WriteString("</item>\n")
		} else if regexp.MustCompile(`^\d+\.`).MatchString(line) {
			// Handle numbered list items
			listContent := regexp.MustCompile(`^\d+\.\s*`).ReplaceAllString(line, "")
			xml.WriteString("    <item type=\"numbered\">")
			xml.WriteString(html.EscapeString(listContent))
			xml.WriteString("</item>\n")
		} else {
			xml.WriteString("    <paragraph>")
			xml.WriteString(html.EscapeString(line))
			xml.WriteString("</paragraph>\n")
		}
	}

	xml.WriteString("</document>")
	return xml.String()
}

// formatAsMarkdown converts plain text content to Markdown format
func (a *Activity) formatAsMarkdown(content string) string {
	var md strings.Builder

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			md.WriteString("\n")
			continue
		}

		// Convert Subject to header
		if strings.HasPrefix(line, "Subject:") {
			md.WriteString("# ")
			md.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "Subject:")))
			md.WriteString("\n\n")
			continue
		}

		// Keep bullet points as-is (already markdown format)
		if strings.HasPrefix(line, "•") || strings.HasPrefix(line, "*") {
			md.WriteString("- ")
			listContent := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "•"), "*"))
			md.WriteString(listContent)
			md.WriteString("\n")
			continue
		}

		// Convert numbered lists to markdown format
		if regexp.MustCompile(`^\d+\.`).MatchString(line) {
			md.WriteString(line)
			md.WriteString("\n")
			continue
		}

		// Regular paragraphs
		md.WriteString(line)
		md.WriteString("\n\n")
	}

	return md.String()
}

// processHandlebarsTemplate processes templates using Handlebars-style syntax
func (a *Activity) processHandlebarsTemplate(templateContent string, data map[string]interface{}, strictMode bool) (string, []string, error) {
	// Convert Handlebars syntax to Go template syntax
	goTemplate := a.convertHandlebarsToGo(templateContent)
	return a.processGoTemplate(goTemplate, data, strictMode)
}

// convertHandlebarsToGo converts Handlebars syntax to Go template syntax
func (a *Activity) convertHandlebarsToGo(template string) string {
	// Replace {{variable}} with {{.variable}}
	re := regexp.MustCompile(`\{\{([^}]+)\}\}`)
	template = re.ReplaceAllStringFunc(template, func(match string) string {
		variable := strings.TrimSpace(match[2 : len(match)-2])
		if !strings.HasPrefix(variable, ".") && !strings.Contains(variable, " ") {
			return "{{." + variable + "}}"
		}
		return match
	})
	return template
}

// getCurrentTemplateType returns the current template type being processed
func (a *Activity) getCurrentTemplateType() string {
	if a.currentTemplateType == "" {
		return "custom"
	}
	return a.currentTemplateType
}

// detectContentType analyzes content and returns appropriate MIME type
func (a *Activity) detectContentType(content string) string {
	content = strings.TrimSpace(content)

	// Check for JSON
	if (strings.HasPrefix(content, "{") && strings.HasSuffix(content, "}")) ||
		(strings.HasPrefix(content, "[") && strings.HasSuffix(content, "]")) {
		var obj interface{}
		if json.Unmarshal([]byte(content), &obj) == nil {
			return "application/json"
		}
	}

	// Check for XML
	if strings.HasPrefix(content, "<?xml") ||
		(strings.HasPrefix(content, "<") && strings.HasSuffix(content, ">")) {
		return "application/xml"
	}

	// Check for HTML
	if strings.Contains(content, "<html>") || strings.Contains(content, "<!DOCTYPE") {
		return "text/html"
	}

	// Check for email format
	if strings.Contains(content, "Subject:") {
		return "message/rfc822"
	}

	// Check for Markdown
	if strings.Contains(content, "# ") || strings.Contains(content, "## ") ||
		strings.Contains(content, "- ") || strings.Contains(content, "* ") {
		return "text/markdown"
	}

	// Default to plain text
	return "text/plain"
}

// analyzeContentStructure provides detailed analysis of content structure
func (a *Activity) analyzeContentStructure(content string) map[string]interface{} {
	lines := strings.Split(content, "\n")
	structure := map[string]interface{}{
		"totalLines":      len(lines),
		"emptyLines":      0,
		"hasSubject":      false,
		"hasListItems":    false,
		"hasNumberedList": false,
		"hasBulletList":   false,
		"hasEmailFormat":  false,
		"hasGreeting":     false,
		"hasSignature":    false,
		"sections":        []string{},
	}

	bulletRegex := regexp.MustCompile(`^\s*[•*]\s`)
	numberedRegex := regexp.MustCompile(`^\s*\d+\.\s`)

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			structure["emptyLines"] = structure["emptyLines"].(int) + 1
			continue
		}

		// Check for subject line
		if strings.HasPrefix(line, "Subject:") {
			structure["hasSubject"] = true
			structure["hasEmailFormat"] = true
		}

		// Check for greeting patterns
		if strings.Contains(strings.ToLower(line), "hello") ||
			strings.Contains(strings.ToLower(line), "dear") ||
			strings.Contains(strings.ToLower(line), "hi ") {
			structure["hasGreeting"] = true
		}

		// Check for signature patterns
		if strings.Contains(strings.ToLower(line), "sincerely") ||
			strings.Contains(strings.ToLower(line), "best regards") ||
			strings.Contains(strings.ToLower(line), "thank you") {
			structure["hasSignature"] = true
		}

		// Check for list items
		if bulletRegex.MatchString(line) {
			structure["hasListItems"] = true
			structure["hasBulletList"] = true
		}

		if numberedRegex.MatchString(line) {
			structure["hasListItems"] = true
			structure["hasNumberedList"] = true
		}

		// Detect sections (lines that end with colon)
		if strings.HasSuffix(line, ":") && len(line) > 1 {
			structure["sections"] = append(structure["sections"].([]string), line)
		}
	}

	// Determine document type
	documentType := "unknown"
	if structure["hasEmailFormat"].(bool) {
		documentType = "email"
	} else if structure["hasListItems"].(bool) {
		documentType = "document-with-lists"
	} else if len(structure["sections"].([]string)) > 0 {
		documentType = "structured-document"
	} else {
		documentType = "plain-text"
	}

	structure["documentType"] = documentType
	structure["complexity"] = a.calculateContentComplexity(structure)

	return structure
}

// calculateContentComplexity calculates a complexity score for the content
func (a *Activity) calculateContentComplexity(structure map[string]interface{}) string {
	score := 0

	if structure["hasSubject"].(bool) {
		score += 1
	}
	if structure["hasListItems"].(bool) {
		score += 2
	}
	if structure["hasGreeting"].(bool) {
		score += 1
	}
	if structure["hasSignature"].(bool) {
		score += 1
	}
	if len(structure["sections"].([]string)) > 0 {
		score += len(structure["sections"].([]string))
	}
	if structure["totalLines"].(int) > 10 {
		score += 1
	}
	if structure["totalLines"].(int) > 20 {
		score += 1
	}

	switch {
	case score <= 2:
		return "simple"
	case score <= 5:
		return "moderate"
	case score <= 8:
		return "complex"
	default:
		return "highly-complex"
	}
}
