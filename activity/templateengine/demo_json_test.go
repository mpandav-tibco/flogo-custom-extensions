package templateengine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/project-flogo/core/support/test"
)

func TestDemoAppJSONFormat(t *testing.T) {
	// Test the exact configuration used in the demo app template-engine-demo.flogo
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

	// Exact same template data from the demo app
	templateData := map[string]interface{}{
		"customerName": "John Doe",
		"companyName":  "ACME Corp",
		"accountDetails": map[string]interface{}{
			"username":  "johndoe",
			"accountId": "12345",
		},
		"gettingStartedSteps": []map[string]interface{}{
			{
				"stepNumber":  1,
				"description": "Complete your profile",
			},
			{
				"stepNumber":  2,
				"description": "Explore our features",
			},
		},
	}

	activityCtx := test.NewActivityContextWithAction(activityMd, nil)

	// Set inputs exactly as in demo app
	activityCtx.SetInput("templateType", "email-welcome")
	activityCtx.SetInput("outputFormat", "json")
	activityCtx.SetInput("enableFormatting", true)
	activityCtx.SetInput("escapeHtml", false)
	activityCtx.SetInput("strictMode", false)
	activityCtx.SetInput("template", "")
	activityCtx.SetInput("templateData", templateData)
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

	// Verify it's valid JSON
	var jsonObj map[string]interface{}
	err = json.Unmarshal([]byte(result), &jsonObj)
	if err != nil {
		t.Fatalf("Demo app result should be valid JSON: %v", err)
	}

	// Check JSON structure
	if content, ok := jsonObj["content"].(string); !ok || content == "" {
		t.Error("JSON should contain 'content' field with template output")
	} else {
		// Verify the content contains expected customer data
		if !strings.Contains(content, "John Doe") {
			t.Error("JSON content should contain customer name")
		}
		if !strings.Contains(content, "ACME Corp") {
			t.Error("JSON content should contain company name")
		}
		if !strings.Contains(content, "johndoe") {
			t.Error("JSON content should contain username")
		}
		if !strings.Contains(content, "12345") {
			t.Error("JSON content should contain account ID")
		}
	}

	if format, ok := jsonObj["format"].(string); !ok || format != "text" {
		t.Error("JSON should contain 'format' field with value 'text'")
	}

	if timestamp, ok := jsonObj["timestamp"].(string); !ok || timestamp == "" {
		t.Error("JSON should contain 'timestamp' field")
	}

	t.Logf("Demo App JSON Output:\n%s", result)

	// Success message
	t.Log("✅ JSON formatting is now working correctly for the demo app!")
	t.Log("✅ The issue has been fixed - plain text output is now properly wrapped in JSON structure")
	t.Log("✅ Demo app will now show properly formatted JSON output instead of plain text")
}
