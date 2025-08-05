package templateengine

import (
	"testing"
)

func TestOutputFormats(t *testing.T) {
	// Create a sample template output
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
• Contact Us: #

If you have any questions, our support team is here to help at support@company.com.

Welcome aboard!

The Team
ACME Corp`

	activity := &Activity{}

	// Test HTML formatting
	htmlOutput, err := activity.formatOutput(sampleContent, "html")
	if err != nil {
		t.Fatalf("HTML formatting failed: %v", err)
	}
	t.Logf("HTML Output:\n%s\n", htmlOutput)

	// Test XML formatting
	xmlOutput, err := activity.formatOutput(sampleContent, "xml")
	if err != nil {
		t.Fatalf("XML formatting failed: %v", err)
	}
	t.Logf("XML Output:\n%s\n", xmlOutput)

	// Test Markdown formatting
	markdownOutput, err := activity.formatOutput(sampleContent, "markdown")
	if err != nil {
		t.Fatalf("Markdown formatting failed: %v", err)
	}
	t.Logf("Markdown Output:\n%s\n", markdownOutput)

	// Test plain text (no formatting)
	textOutput, err := activity.formatOutput(sampleContent, "text")
	if err != nil {
		t.Fatalf("Text formatting failed: %v", err)
	}
	t.Logf("Text Output:\n%s\n", textOutput)

	// Verify outputs are different
	if htmlOutput == textOutput {
		t.Error("HTML output should be different from text output")
	}
	if xmlOutput == textOutput {
		t.Error("XML output should be different from text output")
	}
	if markdownOutput == textOutput {
		t.Error("Markdown output should be different from text output")
	}
}
