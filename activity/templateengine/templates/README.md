# Template Engine Templates

This directory contains the Out-of-the-Box (OOTB) templates used by the Template Engine Flogo activity.

## 📁 Available Templates

| Template File | Template Type | Description |
|---------------|---------------|-------------|
| `email-apology.tmpl` | `email-apology` | Customer service apology emails |
| `email-welcome.tmpl` | `email-welcome` | Welcome new customers |
| `email-order-confirmation.tmpl` | `email-order-confirmation` | Order confirmations with details |
| `email-order-update.tmpl` | `email-order-update` | Order status updates (shipped/delayed/delivered) |
| `email-promotional.tmpl` | `email-promotional` | Marketing and promotional emails |
| `report-summary.tmpl` | `report-summary` | Business reports with metrics |
| `notification-alert.tmpl` | `notification-alert` | System alerts and notifications |
| `invoice-template.tmpl` | `invoice-template` | Professional invoices |
| `contract-template.tmpl` | `contract-template` | Service agreements and contracts |

## 🛠️ Customizing Templates

### Modifying Existing Templates
1. Open any `.tmpl` file in this directory
2. Edit the template content using Go template syntax
3. Save the file - changes will be picked up automatically

### Adding New Templates
1. Create a new `.tmpl` file in this directory
2. Use Go template syntax for variables: `{{.variableName}}`
3. Update the activity descriptor to include the new template type

### Template Syntax Guide

**Variables:**
```go
{{.customerName}}              // Simple variable
{{.customerName | default "Guest"}}  // Variable with default value
```

**Conditionals:**
```go
{{if .orderNumber}}
Order #{{.orderNumber}}
{{end}}

{{if eq .status "shipped"}}
Your order has shipped!
{{else}}
Your order is being processed.
{{end}}
```

**Loops:**
```go
{{range .items}}
• {{.name}} - ${{.price}}
{{end}}
```

**Functions:**
```go
{{.title | upper}}             // Convert to uppercase
{{.date | default "_date"}}    // Use current date if empty
{{printf "%.2f" .price}}       // Format numbers
```

## 📝 Template Variables

Each template expects different variables. Check the template content to see what variables are available. Common variables include:

- `customerName` - Customer's name
- `companyName` - Your company name
- `orderNumber` - Order reference number
- `supportEmail` - Support contact email
- `_date` - Current date (auto-generated)
- `_time` - Current time (auto-generated)

## 🔧 Best Practices

1. **Always provide defaults** for optional variables using `| default "value"`
2. **Test templates** with sample data before deploying
3. **Keep backups** of customized templates
4. **Use meaningful variable names** in your data objects
5. **Document custom variables** for future reference

## 🚀 Usage in Flogo

1. Set `templateType` to one of the template types above
2. Provide data in `templateData` that matches the template variables
3. The activity will automatically load the template from this directory

Example:
```json
{
  "templateType": "email-welcome",
  "templateData": {
    "customerName": "John Doe",
    "companyName": "ACME Corp",
    "supportEmail": "support@acme.com"
  }
}
```
