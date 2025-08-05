package templateengine

import (
	"testing"
)

// Test to verify exact count of template functions
func TestTemplateFunctionCount(t *testing.T) {
	activity := &Activity{}
	funcMap := activity.getTemplateFunctions()

	expectedFunctions := []string{
		// Original 12 functions
		"upper", "lower", "title", "trim", "replace", "contains",
		"join", "split", "now", "formatDate", "default", "json",

		// Enhanced string functions
		"capitalize", "truncate", "reverse",

		// Math functions
		"add", "subtract", "multiply", "divide",

		// Array functions
		"first", "last", "length", "sort",

		// Conditional functions
		"eq", "ne", "lt", "gt", "le", "ge",
	}

	// Verify all expected functions exist
	for _, funcName := range expectedFunctions {
		if _, exists := funcMap[funcName]; !exists {
			t.Errorf("Expected function %s not found", funcName)
		}
	}

	// Check total count
	actualCount := len(funcMap)
	expectedCount := len(expectedFunctions)

	t.Logf("Template Functions Implemented: %d", actualCount)
	t.Logf("Expected Functions: %d", expectedCount)

	if actualCount != expectedCount {
		t.Errorf("Function count mismatch. Expected %d, got %d", expectedCount, actualCount)

		// Log all actual functions for debugging
		t.Logf("All functions in funcMap:")
		for name := range funcMap {
			t.Logf("  - %s", name)
		}
	}
}
