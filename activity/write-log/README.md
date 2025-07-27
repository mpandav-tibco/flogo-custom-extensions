# Write Log Activity - Enterprise Edition

![Write Log Activity](icons/log-file@2x.png)

This Flogo activity provides enterprise-grade structured logging with OpenTracing/OpenTelemetry integration, ECS (Elastic Common Schema) compliance, field filtering, and sensitive data masking capabilities. It offers advanced formatting options and seamless integration with modern logging infrastructures.

## Configuration

### Settings

| Setting | Type | Required | Description | Default |
|---------|------|----------|-------------|---------|
| logLevel | string | Yes | Default log level for this activity (can be overridden by input) | INFO |
| includeFlowInfo | boolean | No | Include ECS Standard Fields - If true, automatically merges standard fields like @timestamp, service.name, etc., into the log. User-provided data takes precedence | true |
| outputFormat | string | Yes | Default format for log output (can be overridden by input) | JSON |
| addFlowDetails | boolean | No | If true, appends flow instance ID, flow name, and activity name to log messages | false |

### Inputs

| Input | Type | Required | Description | Default |
|-------|------|----------|-------------|---------|
| logObject | object | No | Define a JSON schema or object here. The Flogo UI will create mappable fields based on its structure | {"$schema":"http://json-schema.org/draft-04/schema#","type":"object","properties":{"field1":{"type":"string"},"field2":{"type":"number"}}} |
| logLevel | string | No | Override default log level ('TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL') | - |
| sensitiveFields | object | No | Configuration for field masking with properties: fieldNamesToHide (array), maskWith (string), maskLength (number) | {"$schema":"http://json-schema.org/draft-04/schema#","type":"object","properties":{"fieldNamesToHide":{"type":"array","items":{"type":"string"}},"maskWith":{"type":"string","default":"*****"},"maskLength":{"type":"number","default":0}}} |
| fieldFilters | object | No | Field filtering configuration with properties: include (array), exclude (array) | {"$schema":"http://json-schema.org/draft-04/schema#","type":"object","properties":{"include":{"type":"array","items":{"type":"string"}},"exclude":{"type":"array","items":{"type":"string"}}}} |

### Outputs

This activity does not return any outputs. It performs logging operations directly to the configured logging system.

**Note**: The activity intelligently processes inputs based on configuration. When Include ECS Standard Fields is enabled, additional enterprise metadata like ECS fields are automatically added. The activity supports inline flow formatting similar to the official Log activity when addFlowDetails is enabled.

## Usage Examples

### Example 1: Basic Enterprise Logging

```json
{
  "id": "enterprise_log",
  "name": "Write Enterprise Log",
  "activity": {
    "ref": "github.com/milindpandav/activity/write-log",
    "settings": {
      "logLevel": "INFO",
      "outputFormat": "JSON",
      "includeFlowInfo": true,
      "addFlowDetails": true
    },
    "input": {
      "logObject": {
        "event": "user_login",
        "user_id": "12345",
        "session_id": "abc-def-ghi",
        "ip_address": "192.168.1.100"
      }
    }
  }
}
```

### Example 2: Sensitive Data Masking

```json
{
  "id": "secure_log",
  "name": "Log with Data Masking",
  "activity": {
    "ref": "github.com/milindpandav/activity/write-log",
    "settings": {
      "logLevel": "INFO",
      "outputFormat": "JSON",
      "includeFlowInfo": true
    },
    "input": {
      "logObject": {
        "transaction_id": "txn_12345",
        "user_email": "user@example.com",
        "credit_card": "1234-5678-9012-3456",
        "amount": 99.99
      },
      "sensitiveFields": {
        "fieldNamesToHide": ["user_email", "credit_card"],
        "maskWith": "****",
        "maskLength": 4
      }
    }
  }
}
```

### Example 3: Key-Value Format with Field Filtering

```json
{
  "id": "filtered_log",
  "name": "Filtered Key-Value Log",
  "activity": {
    "ref": "github.com/milindpandav/activity/write-log",
    "settings": {
      "outputFormat": "KEY_VALUE",
      "addFlowDetails": false,
      "includeFlowInfo": false
    },
    "input": {
      "logObject": {
        "request_id": "req_789",
        "method": "POST",
        "endpoint": "/api/users",
        "response_time": 156,
        "status_code": 200,
        "internal_debug": "sensitive_info"
      },
      "fieldFilters": {
        "include": ["request_id", "method", "endpoint", "response_time", "status_code"]
      }
    }
  }
}
```

### Example 4: LOGFMT Format with Log Level Override

```json
{
  "id": "error_log",
  "name": "Error Log with Context",
  "activity": {
    "ref": "github.com/milindpandav/activity/write-log",
    "settings": {
      "logLevel": "INFO",
      "outputFormat": "LOGFMT",
      "includeFlowInfo": true
    },
    "input": {
      "logLevel": "ERROR",
      "logObject": {
        "message": "Database connection failed",
        "error_code": "DB_CONN_001",
        "database": "user_db",
        "retry_count": 3,
        "last_error": "Connection timeout after 30s"
      }
    }
  }
}
```

## Supported Output Formats

### JSON Format (Default)
Structured JSON output with full metadata and ECS compliance:

```json
{
  "@timestamp": "2025-01-27T10:30:45.123Z",
  "ecs": {"version": "8.11.0"},
  "log": {
    "level": "INFO",
    "logger": "flogo.activity.write-log"
  },
  "event": {
    "category": ["application"],
    "kind": "event",
    "type": ["info"],
    "dataset": "flogo.application.log"
  },
  "user_data": {
    "event": "user_login",
    "user_id": "12345",
    "session_id": "abc-def-ghi"
  },
  "service": {
    "name": "my-flogo-app",
    "type": "flogo-application",
    "version": "1.0.0"
  },
  "trace": {
    "id": "1234567890abcdef",
    "span": {"id": "abcdef1234567890"}
  }
} [Flow: MyFlow (instance: flow_123), Activity: enterprise_log]
```

### KEY_VALUE Format
Space-separated key=value pairs:

```
timestamp=2025-01-27T10:30:45.123Z level=INFO event.category=application user_data.event=user_login user_data.user_id=12345 service.name=my-flogo-app trace.id=1234567890abcdef [Flow: MyFlow (instance: flow_123), Activity: enterprise_log]
```

### LOGFMT Format
Logfmt-style structured logging:

```
ts=2025-01-27T10:30:45.123Z level=info event=user_login user_id=12345 session_id=abc-def-ghi service=my-flogo-app trace_id=1234567890abcdef [Flow: MyFlow (instance: flow_123), Activity: enterprise_log]
```

## Supported Log Levels

The activity supports the following log levels (configurable via settings or input override):

- `TRACE` - Most detailed logging level
- `DEBUG` - Detailed information for debugging
- `INFO` - General information messages
- `WARN` - Warning messages for potential issues
- `ERROR` - Error messages for failures
- `FATAL` - Critical errors that may cause application termination

## Field Management Features

### Sensitive Data Masking
The `sensitiveFields` input allows you to configure field masking:

```json
{
  "fieldNamesToHide": ["password", "credit_card", "ssn"],
  "maskWith": "****",
  "maskLength": 4
}
```

- **fieldNamesToHide**: Array of field names to mask in the output
- **maskWith**: String used for masking (default: "*****")
- **maskLength**: Number of characters to show before masking (default: 0)

### Field Filtering
The `fieldFilters` input supports include/exclude operations:

```json
{
  "include": ["user_id", "event_type", "timestamp"],
  "exclude": ["internal_debug", "temp_data"]
}
```

- **include**: Only these fields will be included in the output
- **exclude**: These fields will be excluded from the output
- **Nested Field Support**: Use dot notation like "user.profile.email"

## Sample Log Object Schema

```json
{
  "$schema": "http://json-schema.org/draft-04/schema#",
  "type": "object",
  "properties": {
    "event_type": {"type": "string"},
    "user_id": {"type": "string"},
    "session_id": {"type": "string"},
    "timestamp": {"type": "string"},
    "data": {
      "type": "object",
      "properties": {
        "action": {"type": "string"},
        "resource": {"type": "string"},
        "result": {"type": "string"}
      }
    },
    "metadata": {
      "type": "object",
      "properties": {
        "source": {"type": "string"},
        "version": {"type": "string"}
      }
    }
  }
}
```

## Enterprise Features

### ECS (Elastic Common Schema) Compliance
When Include ECS Standard Fields (`includeFlowInfo`) is enabled, the activity automatically adds:

- **Standardized Fields**: Full ECS v8.11 compliance for enterprise log aggregation
- **Agent Metadata**: `agent.name`, `agent.type`, `agent.version`
- **Event Classification**: `event.category`, `event.kind`, `event.type`, `event.dataset`
- **Service Details**: `service.name`, `service.type`, `service.version`
- **Host Information**: `host.name` for infrastructure correlation
- **Process Metadata**: `process.name` for application identification

### OpenTracing/OpenTelemetry Integration
- **Automatic Context Detection**: Detects active tracing contexts from Flogo runtime
- **Trace Correlation**: Adds `trace.id`, `span.id` for distributed tracing
- **Context-Aware Logging**: Leverages Flogo's context-aware logging capabilities

### Flow Integration
When `addFlowDetails` is enabled:
- **Inline Flow Information**: Appends flow details as suffix (like official Log activity)
- **Flow Instance ID**: Includes unique flow execution identifier
- **Activity Context**: Adds current activity name and position

## Error Handling

The activity provides comprehensive error handling:

- **INVALID_INPUT** - Invalid or missing required input parameters
- **SERIALIZATION_ERROR** - Error during JSON serialization
- **FORMATTING_ERROR** - Error during output formatting
- **TRACING_ERROR** - Error accessing tracing context (non-fatal)
- **ECS_ERROR** - Error adding ECS metadata (non-fatal)
- **MASKING_ERROR** - Error during field masking (non-fatal)

All errors are logged but do not stop activity execution to ensure logging reliability.

## Testing

Run the unit tests:

```bash
go test -v
```

Run specific test scenarios:

```bash
# Test basic functionality
go test -v -run TestBasicLogging

# Test output formats
go test -v -run TestOutputFormats

# Test field masking
go test -v -run TestSensitiveDataMasking

# Test field filtering
go test -v -run TestFieldFiltering

# Test ECS compliance
go test -v -run TestECSCompliance
```

## Dependencies

- `github.com/project-flogo/core` v1.6.13+ - Flogo core framework
- `github.com/stretchr/testify` v1.10.0+ - Testing framework

## Environment Variables

The activity respects the following environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `FLOGO_DYNAMICLOG_LOG_LEVEL` | Override default log level | - |
| `FLOGO_LOGACTIVITY_LOG_LEVEL` | Activity-specific log level override | - |
| `FLOGO_APP_NAME` | Application name for service metadata | "flogo-app" |
| `FLOGO_APP_VERSION` | Application version for service metadata | "1.0.0" |

## Migration from Standard Log Activity

To migrate from the standard Flogo Log activity:

### 1. Replace Activity Reference

```json
// Old
"ref": "github.com/tibco/flogo-general/src/app/General/activity/log"

// New
"ref": "github.com/milindpandav/activity/write-log"
```

### 2. Update Configuration

```json
// Old
"input": {
  "message": "User login successful",
  "Log Level": "INFO",
  "flowInfo": true
}

// New
"settings": {
  "logLevel": "INFO",
  "addFlowDetails": true,
  "includeFlowInfo": true
},
"input": {
  "logObject": {
    "message": "User login successful"
  }
}
```

### 3. Configure Enterprise Features

```json
"settings": {
  "includeFlowInfo": true,
  "addFlowDetails": true,
  "outputFormat": "JSON"
}
```

## Notes

- Flow details are appended as a human-readable suffix when `addFlowDetails` is enabled
- ECS metadata is automatically added when Include ECS Standard Fields (`includeFlowInfo`) is enabled
- Tracing context is automatically detected and included when available
- All timestamps use RFC3339 format with microsecond precision
- Field masking and filtering apply to all output formats consistently
- The activity is designed to never fail - errors in non-critical features are logged but don't stop execution
- Compatible with all major logging infrastructures and observability platforms

## Usage Examples

### Example 1: Basic Enterprise Logging

```json
{
  "id": "enterprise_log",
  "name": "Write Enterprise Log",
  "activity": {
    "ref": "github.com/milindpandav/activity/write-log",
    "settings": {
      "logLevel": "INFO",
      "outputFormat": "JSON",
      "addFlowDetails": true,
      "enableECS": true,
      "enableTracing": true
    },
    "input": {
      "logObject": {
        "event": "user_login",
        "user_id": "12345",
        "session_id": "abc-def-ghi",
        "ip_address": "192.168.1.100"
      }
    },
    "output": {
      "logResult": "=$.logEntry"
    }
  }
}
```

### Example 2: Sensitive Data Masking

```json
{
  "id": "secure_log",
  "name": "Log with Data Masking",
  "activity": {
    "ref": "github.com/milindpandav/activity/write-log",
    "settings": {
      "logLevel": "INFO",
      "outputFormat": "JSON",
      "enableMasking": true
    },
    "input": {
      "logObject": {
        "transaction_id": "txn_12345",
        "user_email": "user@example.com",
        "credit_card": "1234-5678-9012-3456",
        "amount": 99.99
      },
      "sensitiveFields": ["user_email", "credit_card"]
    },
    "output": {
      "logResult": "=$.logEntry"
    }
  }
}
```

### Example 3: Key-Value Format with Field Filtering

```json
{
  "id": "filtered_log",
  "name": "Filtered Key-Value Log",
  "activity": {
    "ref": "github.com/milindpandav/activity/write-log",
    "settings": {
      "outputFormat": "KEY_VALUE",
      "addFlowDetails": false
    },
    "input": {
      "logObject": {
        "request_id": "req_789",
        "method": "POST",
        "endpoint": "/api/users",
        "response_time": 156,
        "status_code": 200,
        "internal_debug": "sensitive_info"
      },
      "fieldFilters": ["request_id", "method", "endpoint", "response_time", "status_code"]
    },
    "output": {
      "logResult": "=$.logEntry"
    }
  }
}
```

### Example 4: Error Logging with Additional Context

```json
{
  "id": "error_log",
  "name": "Error Log with Context",
  "activity": {
    "ref": "github.com/milindpandav/activity/write-log",
    "settings": {
      "logLevel": "ERROR",
      "outputFormat": "LOGFMT",
      "enableECS": true
    },
    "input": {
      "logLevel": "ERROR",
      "logObject": "Database connection failed",
      "additionalFields": {
        "error_code": "DB_CONN_001",
        "database": "user_db",
        "retry_count": 3,
        "last_error": "Connection timeout after 30s"
      }
    },
    "output": {
      "logResult": "=$.logEntry"
    }
  }
}
```

## Output Formats

### JSON Format (Default)
Structured JSON output with full metadata and ECS compliance:

```json
{
  "@timestamp": "2025-01-27T10:30:45.123Z",
  "ecs": {"version": "8.11.0"},
  "log": {
    "level": "INFO",
    "logger": "flogo.activity.write-log"
  },
  "event": {
    "category": ["application"],
    "kind": "event",
    "type": ["info"],
    "dataset": "flogo.application.log"
  },
  "user_data": {
    "event": "user_login",
    "user_id": "12345",
    "session_id": "abc-def-ghi"
  },
  "service": {
    "name": "my-flogo-app",
    "type": "flogo-application",
    "version": "1.0.0"
  },
  "trace": {
    "id": "1234567890abcdef",
    "span": {"id": "abcdef1234567890"}
  }
} [Flow: MyFlow (instance: flow_123), Activity: enterprise_log]
```

### KEY_VALUE Format
Space-separated key=value pairs:

```
timestamp=2025-01-27T10:30:45.123Z level=INFO event.category=application user_data.event=user_login user_data.user_id=12345 service.name=my-flogo-app trace.id=1234567890abcdef [Flow: MyFlow (instance: flow_123), Activity: enterprise_log]
```

### LOGFMT Format
Logfmt-style structured logging:

```
ts=2025-01-27T10:30:45.123Z level=info event=user_login user_id=12345 session_id=abc-def-ghi service=my-flogo-app trace_id=1234567890abcdef [Flow: MyFlow (instance: flow_123), Activity: enterprise_log]
```

## Enterprise Features

### 🏢 ECS (Elastic Common Schema) Compliance
- **Standardized Fields**: Full ECS v8.11 compliance for enterprise log aggregation
- **Agent Metadata**: `agent.name`, `agent.type`, `agent.version`
- **Event Classification**: `event.category`, `event.kind`, `event.type`, `event.dataset`
- **Service Details**: `service.name`, `service.type`, `service.version`
- **Host Information**: `host.name` for infrastructure correlation

### 🔍 OpenTracing/OpenTelemetry Integration
- **Automatic Context Detection**: Detects active tracing contexts
- **Trace Correlation**: Adds `trace.id`, `span.id` for distributed tracing
- **Context-Aware Logging**: Leverages Flogo's context-aware logging capabilities

### 🔒 Sensitive Data Masking
- **Field-Level Masking**: Mask specific fields by name
- **Pattern-Based Masking**: Support for regex patterns (credit cards, emails, etc.)
- **Configurable Mask Character**: Customize masking output

### 🎯 Field Management
- **Include Filters**: Specify which fields to include in output
- **Exclude Filters**: Specify which fields to exclude from output
- **Nested Field Support**: Filter nested object fields using dot notation

### 🎨 Output Format Support
- **JSON**: Structured logs for modern log aggregation systems
- **KEY_VALUE**: Key-value pairs for traditional log parsing
- **LOGFMT**: Logfmt format for streamlined log processing

## Sample Input/Output

### Sample Input Object

```json
{
  "user_id": "user_12345",
  "event_type": "purchase",
  "product": {
    "id": "prod_789",
    "name": "Enterprise License",
    "price": 299.99
  },
  "payment": {
    "method": "credit_card",
    "card_number": "4111-1111-1111-1111",
    "billing_address": {
      "street": "123 Main St",
      "city": "San Francisco",
      "state": "CA",
      "zip": "94105"
    }
  },
  "session": {
    "id": "sess_abc123",
    "ip_address": "192.168.1.100",
    "user_agent": "Mozilla/5.0..."
  }
}
```

### Generated JSON Output (with ECS & Masking)

```json
{
  "@timestamp": "2025-01-27T10:30:45.123456Z",
  "ecs": {
    "version": "8.11.0"
  },
  "log": {
    "level": "INFO",
    "logger": "flogo.activity.write-log"
  },
  "event": {
    "category": ["application"],
    "kind": "event",
    "type": ["info"],
    "dataset": "flogo.application.log"
  },
  "agent": {
    "name": "flogo-engine",
    "type": "flogo-application",
    "version": "1.6.13"
  },
  "service": {
    "name": "my-ecommerce-app",
    "type": "flogo-application",
    "version": "1.0.0"
  },
  "host": {
    "name": "app-server-01"
  },
  "process": {
    "name": "flogo-app"
  },
  "user_data": {
    "user_id": "user_12345",
    "event_type": "purchase",
    "product": {
      "id": "prod_789",
      "name": "Enterprise License",
      "price": 299.99
    },
    "payment": {
      "method": "credit_card",
      "card_number": "****-****-****-1111",
      "billing_address": {
        "street": "123 Main St",
        "city": "San Francisco",
        "state": "CA",
        "zip": "94105"
      }
    },
    "session": {
      "id": "sess_abc123",
      "ip_address": "192.168.1.100",
      "user_agent": "Mozilla/5.0..."
    }
  },
  "trace": {
    "id": "1a2b3c4d5e6f7890",
    "span": {
      "id": "9876543210abcdef"
    }
  }
} [Flow: PurchaseFlow (instance: flow_456789), Activity: log_purchase_event]
```

## Environment Configuration

The activity respects the following environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `FLOGO_DYNAMICLOG_LOG_LEVEL` | Override default log level | - |
| `FLOGO_LOGACTIVITY_LOG_LEVEL` | Activity-specific log level override | - |
| `FLOGO_APP_NAME` | Application name for service metadata | "flogo-app" |
| `FLOGO_APP_VERSION` | Application version for service metadata | "1.0.0" |
| `FLOGO_LOG_FORMAT` | Global output format override | - |

## Error Handling

The activity provides comprehensive error handling with specific error types:

- **INVALID_INPUT** - Invalid or missing required input parameters
- **SERIALIZATION_ERROR** - Error during JSON serialization
- **FORMATTING_ERROR** - Error during output formatting
- **TRACING_ERROR** - Error accessing tracing context (non-fatal)
- **ECS_ERROR** - Error adding ECS metadata (non-fatal)
- **MASKING_ERROR** - Error during field masking (non-fatal)

## Testing

Run the comprehensive test suite:

```bash
# Run all tests
go test -v

# Run specific test categories
go test -v -run TestBasicLogging
go test -v -run TestECSCompliance
go test -v -run TestOutputFormats
go test -v -run TestSensitiveDataMasking
go test -v -run TestFieldFiltering
go test -v -run TestTracingIntegration
go test -v -run TestErrorHandling

# Run benchmarks
go test -v -bench=.
```

## Dependencies

- `github.com/project-flogo/core` v1.6.13+ - Flogo core framework
- `github.com/stretchr/testify` v1.10.0+ - Testing framework

## Performance Characteristics

- **Memory Efficient**: Minimal memory allocation for log processing
- **CPU Optimized**: Efficient field processing and serialization
- **Scalable**: Designed for high-throughput enterprise applications
- **Non-Blocking**: Asynchronous processing where possible
- **Thread-Safe**: Safe for concurrent execution in multi-threaded environments

## Migration from Standard Log Activity

To migrate from the standard Flogo Log activity:

### 1. Replace Activity Reference

```json
// Old
"ref": "github.com/tibco/flogo-general/src/app/General/activity/log"

// New
"ref": "github.com/milindpandav/activity/write-log"
```

### 2. Update Input Mapping

```json
// Old
"input": {
  "message": "=$.logMessage",
  "Log Level": "INFO",
  "flowInfo": true
}

// New
"input": {
  "logObject": "=$.logMessage",
  "logLevel": "INFO"
},
"settings": {
  "addFlowDetails": true
}
```

### 3. Enable Enterprise Features

```json
"settings": {
  "enableECS": true,
  "enableTracing": true,
  "addFlowDetails": true,
  "outputFormat": "JSON"
}
```

## Integration Examples

### 🔍 Elasticsearch/ELK Stack
Configure JSON output format with ECS compliance for seamless Elasticsearch integration:

```json
"settings": {
  "outputFormat": "JSON",
  "enableECS": true,
  "addFlowDetails": false
}
```

### 📊 Prometheus/Grafana
Use structured logging with consistent field names for metric extraction:

```json
"settings": {
  "outputFormat": "LOGFMT",
  "enableECS": true
},
"input": {
  "additionalFields": {
    "metric_name": "api_request_duration",
    "metric_value": 156.7,
    "metric_labels": {
      "endpoint": "/api/users",
      "method": "GET",
      "status": "200"
    }
  }
}
```

### 🕵️ Jaeger/Zipkin
Enable tracing integration for distributed request correlation:

```json
"settings": {
  "enableTracing": true,
  "outputFormat": "JSON"
}
```

### 🚀 Fluentd/Fluent Bit
JSON format provides optimal parsing performance for log shipping:

```json
"settings": {
  "outputFormat": "JSON",
  "enableECS": true,
  "addFlowDetails": true
}
```

## Best Practices

### 📋 General Guidelines
1. **Use Structured Data**: Pass objects rather than strings for better searchability
2. **Enable ECS**: Standardizes fields across your enterprise logging infrastructure
3. **Configure Masking**: Always mask sensitive data like PII, credentials, tokens
4. **Filter Fields**: Use field filters to reduce log volume and improve performance
5. **Consistent Levels**: Use appropriate log levels (INFO for business events, ERROR for failures)

### 🔧 Configuration Recommendations
1. **Production**: Enable ECS and tracing, use JSON format
2. **Development**: Use LOGFMT for readability, enable flow details
3. **High-Volume**: Filter fields, disable flow details for performance
4. **Security-Critical**: Enable masking, filter sensitive fields

### 🎯 Field Naming Conventions
1. Use snake_case for field names
2. Prefix custom fields to avoid ECS conflicts
3. Use consistent field names across your application
4. Group related fields in nested objects

## Visual Examples

### Activity in Flogo Studio

The activity appears in the Flogo Studio palette with the log file icon:

![Activity Icon](icons/log-file.svg)

### Configuration Panel

When configuring the activity, you'll see options for:
- **Log Level**: Dropdown with TRACE, DEBUG, INFO, WARN, ERROR, FATAL
- **Output Format**: JSON, KEY_VALUE, LOGFMT options
- **Enterprise Features**: Checkboxes for ECS, Tracing, Masking
- **Flow Details**: Option to append flow information

## Troubleshooting

### Common Issues

1. **Missing Trace Context**: Ensure OpenTelemetry/Jaeger is properly configured
2. **ECS Fields Missing**: Check that `enableECS` is set to true in settings
3. **Masking Not Working**: Verify field names in `sensitiveFields` match exactly
4. **Performance Issues**: Consider disabling flow details and filtering fields

### Debug Mode

Enable debug logging to troubleshoot issues:

```bash
export FLOGO_LOG_LEVEL=DEBUG
```

## Notes

- Flow details are appended as a human-readable suffix when `addFlowDetails` is enabled
- ECS metadata is automatically added when `enableECS` is enabled
- Tracing context is automatically detected and included when available
- All timestamps use RFC3339 format with microsecond precision
- Field masking applies to all output formats consistently
- The activity is designed to never fail - errors in non-critical features are logged but don't stop execution
- Icons are available in multiple resolutions for different display densities
- Compatible with all major logging infrastructures and observability platforms
