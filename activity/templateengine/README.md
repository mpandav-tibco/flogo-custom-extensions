# Template Engine Activity

![Template Engine Icon](icons/templateengine@2x.png)

A comprehensive Flogo activity for processing dynamic templates with data binding, supporting Go templates with Handlebars/Mustache syntax compatibility. Support features including OpenTelemetry tracing, advanced output formatting, and secure template processing.

## Features

- **Multi-Engine Support**: Go templates (full), Handlebars-Basic, Mustache-Basic with syntax compatibility
- **29 Built-in Functions**: Comprehensive template function library for string, math, array, and conditional operations
- **10 OOTB Templates**: Pre-built predefined templates for emails, reports, contracts, and notifications
- **Advanced Output Formatting**: HTML, XML, JSON, Markdown with proper DOM structure and rich metadata
- **Security Features**: Safe mode operation, HTML escaping, strict mode validation
- **Performance Optimization**: Template caching, intelligent path detection, memory-efficient processing
- **AI Workflow Ready**: Perfect for dynamic content generation in AI-powered enterprise workflows

## Configuration

### Settings

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| templateEngine | string | No | Template engine: "go" (full), "handlebars-basic" (syntax compatible), "mustache-basic" (syntax compatible) | go |
| templateCacheSize | integer | No | Maximum number of cached templates for performance | 100 |
| enableSafeMode | boolean | No | Enable safe mode - restricts to essential functions only | true |
| templatePath | string | No | Custom path for template files. If empty, auto-detection will be used | "" |

### Inputs

| Input | Type | Required | Description | Default |
|-------|------|----------|-------------|---------|
| templateType | string | Yes | OOTB template name or "custom" for custom templates | - |
| template | string | No | Custom template content (when templateType is "custom") | - |
| templateData | object | Yes | Primary data object for template binding | - |
| outputFormat | string | No | Output format: "text", "html", "json", "xml", "markdown" | text |
| enableFormatting | boolean | No | Enable automatic formatting based on output format | true |
| templateVariables | object | No | Additional template variables to merge with templateData | {} |
| escapeHtml | boolean | No | Automatically escape HTML characters in template variables | true |
| strictMode | boolean | No | Fail if template references undefined variables | true |

### Outputs

| Output | Type | Description |
|--------|------|-------------|
| result | string | Generated content from template processing |
| success | boolean | Whether template processing succeeded |
| error | string | Error message if processing failed |
| templateUsed | string | Name of the template that was used |
| processingTime | integer | Processing time in milliseconds |
| variablesUsed | array | List of variables that were used in template processing |

## Template Engine Support

### Go Templates (Recommended) ✅ 
- **Complete Engine**: Native Go `text/template` with all features
- **All 29 Functions**: Complete template function library  
- **Syntax**: `{{.Variable}}`, `{{if .Condition}}`, `{{range .Items}}`
- **Features**: Template caching, safe mode, strict mode

### Handlebars-Basic ⚡ Syntax Compatible
- **Compatibility**: Converts `{{variable}}` to `{{.variable}}`
- **Function Support**: All 29 Go template functions available
- **Limitations**: No block helpers (`{{#if}}`, `{{#each}}`), no partials
- **Use Case**: Simple Handlebars migration compatibility

### Mustache-Basic ⚡ Syntax Compatible  
- **Compatibility**: Converts `{{variable}}` to `{{.variable}}`
- **Function Support**: All 29 Go template functions available
- **Limitations**: No sections (`{{#section}}`), no lambdas, no partials
- **Use Case**: Simple Mustache migration compatibility

**Note**: Handlebars-Basic and Mustache-Basic provide syntax compatibility for simple variable substitution but use the Go template engine internally for processing.

## Template Functions (29 Available)

### String Functions (9)
| Function | Description | Example |
|----------|-------------|---------|
| `upper` | Convert to uppercase | `{{upper .Name}}` → `JOHN` |
| `lower` | Convert to lowercase | `{{lower .Email}}` → `john@email.com` |
| `title` | Title case conversion | `{{title .Name}}` → `John Doe` |
| `capitalize` | Capitalize first letter | `{{capitalize .word}}` → `Hello` |
| `truncate` | Truncate with ellipsis | `{{truncate 10 .Description}}` → `Hello w...` |
| `reverse` | Reverse string | `{{reverse .Text}}` → `olleH` |
| `replace` | Replace substring | `{{replace "old" "new" .Text}}` |
| `contains` | Check substring | `{{contains .Text "search"}}` → `true` |
| `trim` | Remove whitespace | `{{trim .Input}}` |

### Math Functions (4)
| Function | Description | Example |
|----------|-------------|---------|
| `add` | Addition | `{{add .Count 1}}` → `6` |
| `subtract` | Subtraction | `{{subtract .Total .Used}}` → `50` |
| `multiply` | Multiplication | `{{multiply .Price .Quantity}}` → `150.00` |
| `divide` | Division | `{{divide .Total .Count}}` → `25` |

### Array Functions (6)
| Function | Description | Example |
|----------|-------------|---------|
| `first` | Get first element | `{{first .Items}}` → `apple` |
| `last` | Get last element | `{{last .Items}}` → `orange` |
| `length` | Get length | `{{length .Items}}` → `3` |
| `sort` | Sort array | `{{sort .Names}}` → `[alice bob charlie]` |
| `join` | Join with separator | `{{join .Items ", "}}` → `a, b, c` |
| `split` | Split to array | `{{split .Text ","}}` → `[a b c]` |

### Conditional Functions (6)
| Function | Description | Example |
|----------|-------------|---------|
| `eq` | Equal comparison | `{{eq .Status "active"}}` → `true` |
| `ne` | Not equal | `{{ne .Count 0}}` → `true` |
| `lt` | Less than | `{{lt .Age 18}}` → `false` |
| `gt` | Greater than | `{{gt .Score 80}}` → `true` |
| `le` | Less or equal | `{{le .Price 100}}` → `true` |
| `ge` | Greater or equal | `{{ge .Level 5}}` → `true` |

### Utility Functions (4)
| Function | Description | Example |
|----------|-------------|---------|
| `default` | Default value | `{{default "Guest" .Name}}` → `Guest` |
| `json` | Convert to JSON | `{{json .Data}}` → `{"key":"value"}` |
| `formatDate` | Format date/time | `{{formatDate "2006-01-02" .Date}}` |
| `now` | Current timestamp | `{{now}}` → `2024-01-15T10:30:00Z` |

### Safe Mode vs Full Mode

#### 🔒 Safe Mode (Production - Default)
**10 Essential Functions Available:**
- String: `upper`, `lower`, `title`, `trim`
- Utility: `default`, `json`, `now`, `formatDate`
- Conditional: `eq`, `ne`
- Array: `length`

#### 🔓 Full Mode (Development/Trusted)
**All 29 Functions Available** - Safe mode functions plus:
- Advanced string: `capitalize`, `truncate`, `reverse`, `replace`, `contains`
- Math: `add`, `subtract`, `multiply`, `divide`
- Array: `first`, `last`, `sort`, `join`, `split`
- Conditional: `lt`, `gt`, `le`, `ge`

## Output Formats

### Text (Default)
Plain text output with optional automatic formatting:
```
Subject: Welcome to ACME Corp!
Hello John Doe,
Welcome to our platform.
```

### HTML
Complete HTML documents with CSS styling:
```html
<!DOCTYPE html>
<html>
<head><title>Template Output</title></head>
<body>
  <div class="email-subject">Welcome to ACME Corp!</div>
  <p>Hello John Doe,</p>
</body>
</html>
```

### XML
Semantic XML with proper element structure:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<document>
  <subject>Welcome to ACME Corp!</subject>
  <paragraph>Hello John Doe,</paragraph>
</document>
```

### JSON
Rich metadata with content analysis:
```json
{
  "content": "Subject: Welcome...",
  "contentType": "message/rfc822",
  "templateType": "email-welcome",
  "metadata": {
    "contentLength": 479,
    "hasSubject": true,
    "detectedStructure": "email"
  }
}
```

### Markdown
Formatted Markdown with proper syntax:
```markdown
# Welcome to ACME Corp!

Hello John Doe,

Welcome to our platform.
```

## Usage Examples

### Basic Email Template
```json
{
  "templateType": "email-welcome",
  "templateData": {
    "customerName": "John Doe",
    "companyName": "ACME Corp",
    "accountDetails": {
      "username": "johndoe",
      "accountId": "12345"
    }
  },
  "outputFormat": "html"
}
```

### Custom Template with Functions
```json
{
  "templateType": "custom",
  "template": "Hello {{upper .name}}, you have {{length .items}} items. Today is {{formatDate \"2006-01-02\" now}}.",
  "templateData": {
    "name": "john doe",
    "items": ["apple", "banana", "orange"]
  },
  "enableSafeMode": false
}
```

### AI-Generated Content
```json
{
  "templateType": "custom",
  "template": "{{.customerName}}, based on your inquiry about {{.inquiry}}: {{.aiResponse}}",
  "templateData": {
    "customerName": "Jane Smith",
    "inquiry": "product recommendations",
    "aiResponse": "We recommend our premium package based on your usage patterns."
  },
  "outputFormat": "json"
}
```

## Template Syntax Examples

### Go Templates (Full Support)
```go
Subject: Welcome {{.Name}}!

Hello {{.Name}},
{{if .IsVIP}}You are our VIP customer!{{end}}

Account Details:
{{range .Accounts}}
• Account: {{.Number}} ({{.Type}})
{{end}}

Functions: {{upper .Name}} | {{formatDate "2006-01-02" now}}
```

### Handlebars-Basic (Simple Variables Only)
```handlebars
Subject: Welcome {{Name}}!
Hello {{Name}},
Account: {{AccountNumber}}
Note: Only simple {{variable}} substitution works.
Block helpers like {{#if}} are NOT supported.
```

### Mustache-Basic (Simple Variables Only)  
```mustache
Subject: Welcome {{Name}}!
Hello {{Name}},
Account: {{AccountNumber}}
Note: Only simple {{variable}} substitution works.
Sections like {{#Name}} are NOT supported.
```

### Security Features
- **Safe Mode**: Restricts to 10 essential functions for production
- **HTML Escaping**: Prevents XSS attacks with automatic escaping
- **Strict Mode**: Validates all template variables

## Error Handling

The activity provides comprehensive error information:
- Template syntax errors with line numbers
- Data binding failures with missing fields
- HTML escaping warnings for security



## Contributing

### Adding New OOTB Templates
1. Create a new `.tmpl` file in the `/templates/` directory
2. Add the template name to the `allowed` values in `descriptor.json`
3. Test the template with sample data

### Adding New Template Functions
1. Edit the `getTemplateFunctions()` method in `activity.go`
2. Add the function to the template.FuncMap
3. Update this README with documentation and examples
4. Add unit tests to verify the function works correctly

## License

This activity is part of the custom Flogo extensions collection.
