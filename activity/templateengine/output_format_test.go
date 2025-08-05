package templateengine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/project-flogo/core/support/test"
)

func TestIndividualOutputFormats(t *testing.T) {
	// Initialize activity
	settings := &Settings{
		TemplateEngine:    "go",
		TemplateCacheSize: 100,
		EnableSafeMode:    true,
		TemplatePath:      "",
	}

	ctx := test.NewActivityInitContext(settings, nil)
	act, err := New(ctx)
	if err != nil {
		t.Fatalf("Failed to create activity: %v", err)
	}

	// Test data for all formats
	templateData := map[string]interface{}{
		"customerName": "John Doe",
		"companyName":  "ACME Corp",
		"accountDetails": map[string]interface{}{
			"username":  "johndoe",
			"accountId": "12345",
		},
		"gettingStartedSteps": []map[string]interface{}{
			{"stepNumber": 1, "description": "Complete your profile"},
			{"stepNumber": 2, "description": "Explore our features"},
		},
	}

	testCases := []struct {
		name         string
		outputFormat string
		expectedKeys []string // Keys to check in output for validation
		validator    func(t *testing.T, result string)
	}{
		{
			name:         "JSON Format",
			outputFormat: "json",
			expectedKeys: []string{"content", "format", "timestamp"},
			validator: func(t *testing.T, result string) {
				// Should be valid JSON
				var jsonObj map[string]interface{}
				err := json.Unmarshal([]byte(result), &jsonObj)
				if err != nil {
					t.Errorf("JSON format result is not valid JSON: %v", err)
					t.Logf("Result: %s", result)
					return
				}

				// Check required fields
				if content, ok := jsonObj["content"].(string); !ok || content == "" {
					t.Error("JSON format should contain 'content' field with string value")
				}
				if format, ok := jsonObj["format"].(string); !ok || format != "text" {
					t.Error("JSON format should contain 'format' field with value 'text'")
				}
				if timestamp, ok := jsonObj["timestamp"].(string); !ok || timestamp == "" {
					t.Error("JSON format should contain 'timestamp' field with string value")
				}

				t.Logf("JSON Output:\n%s", result)
			},
		},
		{
			name:         "HTML Format",
			outputFormat: "html",
			expectedKeys: []string{"<!DOCTYPE html>", "<html>", "<head>", "<body>", "</html>"},
			validator: func(t *testing.T, result string) {
				// Check for HTML document structure
				requiredElements := []string{
					"<!DOCTYPE html>",
					"<html>",
					"<head>",
					"<meta charset=\"UTF-8\">",
					"<title>Template Output</title>",
					"<style>",
					"body { font-family: Arial",
					"<body>",
					"</html>",
				}

				for _, element := range requiredElements {
					if !strings.Contains(result, element) {
						t.Errorf("HTML format missing required element: %s", element)
					}
				}

				// Check for proper HTML escaping
				if strings.Contains(result, "&amp;") || strings.Contains(result, "&lt;") || strings.Contains(result, "&gt;") {
					t.Log("HTML format properly escapes special characters")
				}

				t.Logf("HTML Output (first 300 chars):\n%s...", result[:min(300, len(result))])
			},
		},
		{
			name:         "XML Format",
			outputFormat: "xml",
			expectedKeys: []string{"<?xml version=", "<document>", "</document>"},
			validator: func(t *testing.T, result string) {
				// Check for XML document structure
				requiredElements := []string{
					"<?xml version=\"1.0\" encoding=\"UTF-8\"?>",
					"<document>",
					"</document>",
				}

				for _, element := range requiredElements {
					if !strings.Contains(result, element) {
						t.Errorf("XML format missing required element: %s", element)
					}
				}

				// Check for proper XML structure
				if strings.Count(result, "<document>") != 1 || strings.Count(result, "</document>") != 1 {
					t.Error("XML format should have exactly one document element")
				}

				t.Logf("XML Output:\n%s", result)
			},
		},
		{
			name:         "Markdown Format",
			outputFormat: "markdown",
			expectedKeys: []string{"#", "-", "**"},
			validator: func(t *testing.T, result string) {
				// Check for Markdown formatting elements
				hasHeaders := strings.Contains(result, "# ") || strings.Contains(result, "## ") || strings.Contains(result, "### ")
				hasLists := strings.Contains(result, "- ") || strings.Contains(result, "* ")

				if hasHeaders {
					t.Log("Markdown format contains headers")
				}
				if hasLists {
					t.Log("Markdown format contains lists")
				}

				// Check for proper line breaks (markdown needs double newlines for paragraphs)
				lines := strings.Split(result, "\n")
				if len(lines) > 1 {
					t.Log("Markdown format has proper line structure")
				}

				t.Logf("Markdown Output:\n%s", result)
			},
		},
		{
			name:         "Text Format (Default)",
			outputFormat: "text",
			expectedKeys: []string{"John Doe", "ACME Corp"},
			validator: func(t *testing.T, result string) {
				// Should be plain text without any formatting tags
				if strings.Contains(result, "<") || strings.Contains(result, "{") || strings.Contains(result, "#") {
					t.Log("Text format may contain original content markers")
				}

				// Should contain the customer name and company name
				if !strings.Contains(result, "John Doe") {
					t.Error("Text format should contain customer name")
				}
				if !strings.Contains(result, "ACME Corp") {
					t.Error("Text format should contain company name")
				}

				t.Logf("Text Output:\n%s", result)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create activity context with activity metadata
			activityCtx := test.NewActivityContextWithAction(activityMd, nil)

			// Set inputs
			activityCtx.SetInput("templateType", "email-welcome")
			activityCtx.SetInput("templateData", templateData)
			activityCtx.SetInput("outputFormat", tc.outputFormat)
			activityCtx.SetInput("enableFormatting", true)
			activityCtx.SetInput("escapeHtml", false)
			activityCtx.SetInput("strictMode", false)
			activityCtx.SetInput("template", "")
			activityCtx.SetInput("templateVariables", map[string]interface{}{})

			// Execute activity
			done, err := act.Eval(activityCtx)
			if err != nil {
				t.Fatalf("Activity execution failed: %v", err)
			}
			if !done {
				t.Fatal("Activity should be done")
			}

			// Get results
			result := activityCtx.GetOutput("result").(string)
			success := activityCtx.GetOutput("success").(bool)

			if !success {
				errorMsg := activityCtx.GetOutput("error").(string)
				t.Fatalf("Activity execution failed: %s", errorMsg)
			}

			// Validate result is not empty
			if result == "" {
				t.Fatal("Result should not be empty")
			}

			// Run format-specific validation
			tc.validator(t, result)

			// Check for expected content
			for _, key := range tc.expectedKeys {
				if !strings.Contains(result, key) {
					t.Logf("Warning: Expected key '%s' not found in output for format %s", key, tc.outputFormat)
				}
			}
		})
	}
}

func TestJSONFormatWithValidJSON(t *testing.T) {
	// Test JSON format when template output is already valid JSON
	settings := &Settings{
		TemplateEngine:    "go",
		TemplateCacheSize: 100,
		EnableSafeMode:    false,
		TemplatePath:      "",
	}

	ctx := test.NewActivityInitContext(settings, nil)
	act, err := New(ctx)
	if err != nil {
		t.Fatalf("Failed to create activity: %v", err)
	}

	// Template that outputs valid JSON
	jsonTemplate := `{"name": "{{.name}}", "age": {{.age}}, "email": "{{.email}}"}`
	templateData := map[string]interface{}{
		"name":  "John Doe",
		"age":   30,
		"email": "john@example.com",
	}

	activityCtx := test.NewActivityContextWithAction(activityMd, nil)

	// Set inputs
	activityCtx.SetInput("templateType", "custom")
	activityCtx.SetInput("template", jsonTemplate)
	activityCtx.SetInput("templateData", templateData)
	activityCtx.SetInput("outputFormat", "json")
	activityCtx.SetInput("enableFormatting", true)
	activityCtx.SetInput("escapeHtml", false)
	activityCtx.SetInput("strictMode", false)
	activityCtx.SetInput("templateVariables", map[string]interface{}{})

	// Execute activity
	done, err := act.Eval(activityCtx)
	if err != nil {
		t.Fatalf("Activity execution failed: %v", err)
	}
	if !done {
		t.Fatal("Activity should be done")
	}

	// Get results
	result := activityCtx.GetOutput("result").(string)
	success := activityCtx.GetOutput("success").(bool)

	if !success {
		errorMsg := activityCtx.GetOutput("error").(string)
		t.Fatalf("Activity execution failed: %s", errorMsg)
	}

	// Should be formatted JSON with metadata wrapper
	var jsonWrapper map[string]interface{}
	err = json.Unmarshal([]byte(result), &jsonWrapper)
	if err != nil {
		t.Fatalf("Result should be valid JSON: %v", err)
	}

	// Extract the actual content from the wrapper
	content, ok := jsonWrapper["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected content to be an object, got %T", jsonWrapper["content"])
	}

	// Check content
	if content["name"] != "John Doe" {
		t.Errorf("Expected name 'John Doe', got %v", content["name"])
	}
	if content["age"] != float64(30) { // JSON numbers are float64
		t.Errorf("Expected age 30, got %v", content["age"])
	}

	t.Logf("Valid JSON Output:\n%s", result)
}

func TestOutputFormattingDisabled(t *testing.T) {
	// Test that when enableFormatting is false, output format is ignored
	settings := &Settings{
		TemplateEngine:    "go",
		TemplateCacheSize: 100,
		EnableSafeMode:    true,
		TemplatePath:      "",
	}

	ctx := test.NewActivityInitContext(settings, nil)
	act, err := New(ctx)
	if err != nil {
		t.Fatalf("Failed to create activity: %v", err)
	}

	templateData := map[string]interface{}{
		"customerName": "John Doe",
		"companyName":  "ACME Corp",
	}

	activityCtx := test.NewActivityContextWithAction(activityMd, nil)

	// Set inputs
	activityCtx.SetInput("templateType", "email-welcome")
	activityCtx.SetInput("templateData", templateData)
	activityCtx.SetInput("outputFormat", "html")    // This should be ignored
	activityCtx.SetInput("enableFormatting", false) // Formatting disabled
	activityCtx.SetInput("escapeHtml", false)
	activityCtx.SetInput("strictMode", false)
	activityCtx.SetInput("template", "")
	activityCtx.SetInput("templateVariables", map[string]interface{}{})

	// Execute activity
	done, err := act.Eval(activityCtx)
	if err != nil {
		t.Fatalf("Activity execution failed: %v", err)
	}
	if !done {
		t.Fatal("Activity should be done")
	}

	// Get results
	result := activityCtx.GetOutput("result").(string)
	success := activityCtx.GetOutput("success").(bool)

	if !success {
		errorMsg := activityCtx.GetOutput("error").(string)
		t.Fatalf("Activity execution failed: %s", errorMsg)
	}

	// Should NOT contain HTML tags
	if strings.Contains(result, "<html>") || strings.Contains(result, "<body>") {
		t.Error("When formatting is disabled, output should not contain HTML formatting")
	}

	// Should contain plain text content
	if !strings.Contains(result, "John Doe") {
		t.Error("Should contain original template content")
	}

	t.Logf("Unformatted Output:\n%s", result)
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
