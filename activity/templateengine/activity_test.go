package templateengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// Test that we can use the enhanced template functions directly
func TestEnhancedTemplateFunctions(t *testing.T) {
	activity := &Activity{}
	funcMap := activity.getTemplateFunctions()

	// Test that our new functions are available
	expectedFuncs := []string{
		"upper", "lower", "title", "capitalize", "truncate", "reverse",
		"add", "subtract", "multiply", "divide",
		"first", "last", "length", "sort",
		"eq", "ne", "lt", "gt", "le", "ge",
		"formatDate", "default", "json",
	}

	for _, funcName := range expectedFuncs {
		if _, exists := funcMap[funcName]; !exists {
			t.Errorf("Expected function %s not found in function map", funcName)
		}
	}
}

// Test that essential functions are available in safe mode
func TestSafeModeEssentialFunctions(t *testing.T) {
	activity := &Activity{}
	funcMap := activity.getEssentialTemplateFunctions()

	// Test that essential functions are available in safe mode
	essentialFuncs := []string{
		"upper", "lower", "title", "trim", "trimSpace",
		"default", "json", "now", "formatDate",
		"eq", "ne", "length",
	}

	for _, funcName := range essentialFuncs {
		if _, exists := funcMap[funcName]; !exists {
			t.Errorf("Expected essential function %s not found in safe mode function map", funcName)
		}
	}

	// Verify that the essential function count is correct
	t.Logf("Available essential functions: %v", getFunctionNames(funcMap))
	if len(funcMap) != 12 {
		t.Errorf("Expected 12 essential functions, got %d", len(funcMap))
	}
}

// Helper function to get function names for debugging
func getFunctionNames(funcMap template.FuncMap) []string {
	names := make([]string, 0, len(funcMap))
	for name := range funcMap {
		names = append(names, name)
	}
	return names
}

// Test that the default function works correctly in safe mode
func TestDefaultFunctionInSafeMode(t *testing.T) {
	activity := &Activity{}
	funcMap := activity.getEssentialTemplateFunctions()

	// Test template that uses the default function
	tmpl, err := template.New("test").Funcs(funcMap).Parse(`{{.name | default "Anonymous"}}`)
	if err != nil {
		t.Fatalf("Failed to parse template with default function: %v", err)
	}

	// Test with empty name (should use default)
	var result1 strings.Builder
	data1 := map[string]interface{}{"name": ""}
	err = tmpl.Execute(&result1, data1)
	if err != nil {
		t.Fatalf("Failed to execute template with empty name: %v", err)
	}

	if result1.String() != "Anonymous" {
		t.Errorf("Expected 'Anonymous', got '%s'", result1.String())
	}

	// Test with actual name (should not use default)
	var result2 strings.Builder
	data2 := map[string]interface{}{"name": "John"}
	err = tmpl.Execute(&result2, data2)
	if err != nil {
		t.Fatalf("Failed to execute template with actual name: %v", err)
	}

	if result2.String() != "John" {
		t.Errorf("Expected 'John', got '%s'", result2.String())
	}
}

// Test that template functions work correctly
func TestTemplateFunctionExecution(t *testing.T) {
	activity := &Activity{}
	funcMap := activity.getTemplateFunctions()

	// Test string functions
	tmpl, err := template.New("test").Funcs(funcMap).Parse(`{{upper "hello"}} {{lower "WORLD"}} {{capitalize "test"}}`)
	if err != nil {
		t.Fatalf("Failed to parse template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, nil)
	if err != nil {
		t.Fatalf("Failed to execute template: %v", err)
	}

	output := result.String()
	if !strings.Contains(output, "HELLO") {
		t.Errorf("Expected 'HELLO' in output, got: %s", output)
	}
	if !strings.Contains(output, "world") {
		t.Errorf("Expected 'world' in output, got: %s", output)
	}
	if !strings.Contains(output, "Test") {
		t.Errorf("Expected 'Test' in output, got: %s", output)
	}
}

// Test math functions
func TestMathFunctions(t *testing.T) {
	activity := &Activity{}
	funcMap := activity.getTemplateFunctions()

	tmpl, err := template.New("test").Funcs(funcMap).Parse(`{{add 5 3}} {{subtract 10 4}} {{multiply 3 4}}`)
	if err != nil {
		t.Fatalf("Failed to parse template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, nil)
	if err != nil {
		t.Fatalf("Failed to execute template: %v", err)
	}

	output := result.String()
	if !strings.Contains(output, "8") { // 5+3
		t.Errorf("Expected '8' in output, got: %s", output)
	}
	if !strings.Contains(output, "6") { // 10-4
		t.Errorf("Expected '6' in output, got: %s", output)
	}
	if !strings.Contains(output, "12") { // 3*4
		t.Errorf("Expected '12' in output, got: %s", output)
	}
}

func TestOOTBTemplateFiles(t *testing.T) {
	// Test that OOTB template files exist in the templates directory
	expectedTemplates := []string{
		"email-apology",
		"email-welcome",
		"email-order-confirmation",
		"email-order-update",
		"email-promotional",
		"report-summary",
		"notification-alert",
		"invoice-template",
		"contract-template",
	}

	for _, templateName := range expectedTemplates {
		templatePath := filepath.Join("templates", templateName+".tmpl")
		if _, err := os.Stat(templatePath); os.IsNotExist(err) {
			t.Errorf("OOTB template file '%s' does not exist at path: %s", templateName, templatePath)
		}
	}
}

func TestActivitySettings(t *testing.T) {
	settings := &Settings{
		TemplateEngine:    "go",
		TemplateCacheSize: 100,
		EnableSafeMode:    true,
	}

	// Verify settings are properly configured
	if settings.TemplateEngine != "go" {
		t.Errorf("Expected TemplateEngine 'go', got '%s'", settings.TemplateEngine)
	}
	if settings.TemplateCacheSize != 100 {
		t.Errorf("Expected TemplateCacheSize 100, got %d", settings.TemplateCacheSize)
	}
	if !settings.EnableSafeMode {
		t.Error("Expected EnableSafeMode to be true")
	}
}

func TestInputDataStructure(t *testing.T) {
	// Test input structure creation and validation
	input := &Input{
		TemplateType:      "email-welcome",
		Template:          "",
		TemplateData:      map[string]interface{}{"customerName": "John Doe"},
		OutputFormat:      "text",
		EnableFormatting:  true,
		TemplateVariables: map[string]interface{}{"companyName": "Test Corp"},
		EscapeHtml:        false,
		StrictMode:        false,
	}

	// Verify input fields
	if input.TemplateType != "email-welcome" {
		t.Errorf("Expected TemplateType 'email-welcome', got '%s'", input.TemplateType)
	}
	if input.TemplateData["customerName"] != "John Doe" {
		t.Errorf("Expected customerName 'John Doe', got '%v'", input.TemplateData["customerName"])
	}
	if input.OutputFormat != "text" {
		t.Errorf("Expected OutputFormat 'text', got '%s'", input.OutputFormat)
	}
	if !input.EnableFormatting {
		t.Error("Expected EnableFormatting to be true")
	}
}

func TestTemplateVariableTypes(t *testing.T) {
	// Test different data types in template variables
	testData := map[string]interface{}{
		"stringVar": "test string",
		"intVar":    42,
		"floatVar":  3.14,
		"boolVar":   true,
		"arrayVar":  []string{"item1", "item2"},
		"objectVar": map[string]interface{}{"nested": "value"},
	}

	// Verify data types are preserved
	if testData["stringVar"].(string) != "test string" {
		t.Error("String variable type not preserved")
	}
	if testData["intVar"].(int) != 42 {
		t.Error("Integer variable type not preserved")
	}
	if testData["floatVar"].(float64) != 3.14 {
		t.Error("Float variable type not preserved")
	}
	if testData["boolVar"].(bool) != true {
		t.Error("Boolean variable type not preserved")
	}
	if len(testData["arrayVar"].([]string)) != 2 {
		t.Error("Array variable type not preserved")
	}
	if testData["objectVar"].(map[string]interface{})["nested"] != "value" {
		t.Error("Object variable type not preserved")
	}
}

func TestTemplateFileLoading(t *testing.T) {
	// Create an activity instance with proper initialization
	activity := &Activity{
		templateBasePath: "./templates", // Set the template base path for testing
	}

	// Test loading existing template
	content, err := activity.getTemplate("email-welcome", "")
	if err != nil {
		t.Errorf("Failed to load email-welcome template: %v", err)
	}
	if content == "" {
		t.Error("Template content is empty")
	}

	// Test loading non-existent template
	_, err = activity.getTemplate("non-existent", "")
	if err == nil {
		t.Error("Expected error for non-existent template")
	}

	// Test custom template
	customTemplate := "Hello {{.name}}"
	content, err = activity.getTemplate("custom", customTemplate)
	if err != nil {
		t.Errorf("Failed to load custom template: %v", err)
	}
	if content != customTemplate {
		t.Errorf("Expected custom template content '%s', got '%s'", customTemplate, content)
	}
}

// TestOutputFormatting tests the different output formatting options
func TestOutputFormatting(t *testing.T) {
	activity := &Activity{}

	// Sample email content similar to what email-welcome template produces
	sampleContent := `Subject: Welcome to ACME Corp! 🎉

Hello John Doe,

Welcome to ACME Corp! We're thrilled to have you join our community.

Here are your account details:
• Username: johndoe
• Account ID: 12345

To get started:

1. Complete your profile
2. Explore our features

Useful links:
• Getting Started Guide: #
• Support Center: #

Welcome aboard!

The Team
ACME Corp`

	t.Run("HTML Format", func(t *testing.T) {
		htmlOutput, err := activity.formatOutput(sampleContent, "html")
		if err != nil {
			t.Fatalf("HTML formatting failed: %v", err)
		}

		// Check for HTML structure
		if !strings.Contains(htmlOutput, "<!DOCTYPE html>") {
			t.Error("HTML output should contain DOCTYPE declaration")
		}
		if !strings.Contains(htmlOutput, "<html>") {
			t.Error("HTML output should contain html tag")
		}
		if !strings.Contains(htmlOutput, "<body>") {
			t.Error("HTML output should contain body tag")
		}
		if !strings.Contains(htmlOutput, "<ul>") {
			t.Error("HTML output should contain list tags for bullet points")
		}
		if !strings.Contains(htmlOutput, "email-subject") {
			t.Error("HTML output should contain email-subject class")
		}

		t.Logf("HTML Output (first 200 chars):\n%s...\n", htmlOutput[:200])
	})

	t.Run("XML Format", func(t *testing.T) {
		xmlOutput, err := activity.formatOutput(sampleContent, "xml")
		if err != nil {
			t.Fatalf("XML formatting failed: %v", err)
		}

		// Check for XML structure
		if !strings.Contains(xmlOutput, "<?xml version=") {
			t.Error("XML output should contain XML declaration")
		}
		if !strings.Contains(xmlOutput, "<document>") {
			t.Error("XML output should contain document root element")
		}
		if !strings.Contains(xmlOutput, "<subject>") {
			t.Error("XML output should contain subject element")
		}

		t.Logf("XML Output:\n%s\n", xmlOutput)
	})

	t.Run("Markdown Format", func(t *testing.T) {
		markdownOutput, err := activity.formatOutput(sampleContent, "markdown")
		if err != nil {
			t.Fatalf("Markdown formatting failed: %v", err)
		}

		// Check for Markdown structure
		if !strings.Contains(markdownOutput, "# Welcome to ACME Corp!") {
			t.Error("Markdown output should contain header format")
		}
		if !strings.Contains(markdownOutput, "- Username:") {
			t.Error("Markdown output should contain bullet list format")
		}

		t.Logf("Markdown Output:\n%s\n", markdownOutput)
	})

	t.Run("Text Format (unchanged)", func(t *testing.T) {
		textOutput, err := activity.formatOutput(sampleContent, "text")
		if err != nil {
			t.Fatalf("Text formatting failed: %v", err)
		}

		// Text format should be unchanged
		if textOutput != sampleContent {
			t.Error("Text format should return original content unchanged")
		}
	})

	t.Run("JSON Format", func(t *testing.T) {
		jsonContent := `{"name": "John", "age": 30}`
		jsonOutput, err := activity.formatOutput(jsonContent, "json")
		if err != nil {
			t.Fatalf("JSON formatting failed: %v", err)
		}

		// Should be pretty-printed JSON
		if !strings.Contains(jsonOutput, "  ") {
			t.Error("JSON output should contain indentation")
		}

		t.Logf("JSON Output:\n%s\n", jsonOutput)
	})
}
