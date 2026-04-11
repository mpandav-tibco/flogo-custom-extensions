# SSE Send Activity

A Flogo activity for sending Server-Sent Events to connected clients through the SSE Trigger server.

## Overview

The SSE Send Activity works in conjunction with the [SSE Trigger](../trigger/README.md) to broadcast real-time events to connected web clients. It provides flexible event targeting, automatic target building, data formatting, and validation capabilities for reliable event delivery.


## Configuration


### Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `sseServerRef` | string | no | "default" | Reference to the SSE server instance |
| `topic` | string | no | "" | Default topic/channel name for events |
| `eventType` | string | no | "message" | Default type of the event |
| `retry` | integer | no | 30000 | Client reconnection timeout in milliseconds |

### Inputs

| Input | Type | Required | Description |
|-------|------|----------|-------------|
| `connectionId` | string | No | Specific connection ID to send event to |
| `target` | string | No | Target for the event: "all", "connection:ID", or "topic:NAME" (defaults to "all") |
| `eventId` | string | No | Unique identifier for the event (auto-generated if not provided) |
| `topic` | string | No | Topic/channel name to send event to (overrides default setting) |
| `eventType` | string | No | Type of the event (overrides default setting) |
| `data` | any | Yes | Event data payload (formatted based on "format" setting) |
| `format` | string | No | Data format: "json", "string", or "auto" (default: "auto") |
| `enableValidation` | boolean | No | Enable input validation (default: true) |

### Outputs

| Output | Type | Description |
|--------|------|-------------|
| `success` | boolean | Whether the event was sent successfully |
| `sentCount` | integer | Number of clients that received the event |
| `eventId` | string | The event ID that was sent (generated if not provided) |
| `timestamp` | string | Timestamp when the event was sent (RFC3339 format) |
| `error` | string | Error message if sending failed |



## Usage Examples

### 1. Broadcast to All Clients

```json
{
  "id": "broadcast_all",
  "activity": {
    "ref": "github.com/milindpandav/flogo-extensions/sse/activity",
    "input": {
      "target": "all",
      "eventType": "notification",
      "data": {
        "message": "System maintenance in 10 minutes",
        "priority": "high"
      },
      "format": "json"
    }
  }
}
```

### 2. Send to Specific Connection

```json
{
  "id": "send_to_connection",
  "activity": {
    "ref": "github.com/milindpandav/flogo-extensions/sse/activity",
    "input": {
      "target": "connection:conn_12345",
      "eventType": "private_message",
      "data": "Hello, this is a private message for you!",
      "format": "string"
    }
  }
}
```

### 3. Send to Topic Subscribers

```json
{
  "id": "send_to_topic",
  "activity": {
    "ref": "github.com/milindpandav/flogo-extensions/sse/activity",
    "input": {
      "target": "topic:chat-room-1",
      "eventType": "chat_message",
      "data": {
        "user": "john_doe",
        "message": "Hello everyone!",
        "timestamp": "2024-01-15T10:30:00Z"
      },
      "retry": 3000
    }
  }
}
```

## Data Formatting

The activity supports three data formatting options that control how event data is serialized:

### JSON Format
Explicitly formats data as JSON:
```json
{
  "format": "json",
  "data": {
    "message": "Hello",
    "timestamp": "2024-01-15T10:30:00Z",
    "priority": "high"
  }
}
```
*Output: `"{\"message\":\"Hello\",\"timestamp\":\"2024-01-15T10:30:00Z\",\"priority\":\"high\"}"`*

### String Format
Converts data to string representation:
```json
{
  "format": "string", 
  "data": "Simple text message"
}
```
*Output: `"Simple text message"`*

For non-strings, uses Go's `fmt.Sprintf("%v", data)`:
```json
{
  "format": "string",
  "data": 42
}
```
*Output: `"42"`*

### Auto Format (Default)
Intelligently determines the best format based on data type:

**Decision Logic:**
1. **Strings**: Sent as-is without modification
   ```json
   "data": "Hello World" // → "Hello World"
   ```

2. **Complex Types** (JSON-encoded):
   - Maps/Objects: `{"key": "value"}`
   - Arrays/Slices: `["item1", "item2"]` 
   - Typed slices: `[]string`, `[]int`, `[]float64`, `[]bool`
   ```json
   "data": {"message": "Hello", "count": 5} // → "{\"message\":\"Hello\",\"count\":5}"
   ```

3. **Simple Types** (String representation):
   - Numbers: `42` → `"42"`
   - Booleans: `true` → `"true"`
   - Other basic types converted using `fmt.Sprintf("%v", data)`

**Auto Format Examples:**
```json
// String data
{"data": "Hello"} // → "Hello"

// Object data
{"data": {"status": "ok"}} // → "{\"status\":\"ok\"}"

// Array data  
{"data": [1, 2, 3]} // → "[1,2,3]"

// Number data
{"data": 42} // → "42"

// Boolean data
{"data": true} // → "true"
```


### Per-Event Retry Override
The retry setting is sent to clients as part of the SSE event format, telling them how long to wait before attempting to reconnect if the connection is lost. This provides reliable reconnection behavior for robust real-time applications.

## Target Formats

The activity supports multiple ways to specify targets, with automatic target building when not explicitly provided:

### Automatic Target Building
If no `target` is specified, the activity automatically builds it based on available inputs:
1. **Connection ID Priority**: `connectionId` input → `"connection:conn_123"`
2. **Input Topic Priority**: `topic` input → `"topic:notifications"`  
3. **Setting Topic Fallback**: activity topic setting → `"topic:default-topic"`
4. **Default Fallback**: No specific target → `"all"`

### Explicit Target Formats

### Broadcast to All
```
"target": "all"
```

### Send to Specific Connection
```
"target": "connection:conn_12345"
```

### Send to Topic Subscribers
```
"target": "topic:chat-room-1"
```

## Error Handling

The activity includes comprehensive error handling:

- **Input validation**: Validates all inputs before processing
- **Server availability**: Checks if SSE server is running
- **Event formatting**: Validates event data format
- **Target parsing**: Validates target format and existence
- **Graceful failures**: Returns error information in output instead of throwing exceptions

## Integration with SSE Trigger

This activity is designed to work seamlessly with the SSE Trigger through a shared registry:

### Server Registry Integration
1. **SSE Trigger** registers itself in the shared registry with a name (e.g., "sse_server")
2. **SSE Send Activity** references the trigger by name using `sseServerRef` setting
3. **Automatic Discovery**: Activity automatically finds and connects to the appropriate server
4. **Multiple Server Support**: Can work with multiple SSE servers by referencing different names

### Usage Pattern
```json
{
  "trigger": {
    "name": "dashboard_sse",
    "ref": "github.com/milindpandav/flogo-extensions/sse/trigger"
  },
  "activity": {
    "ref": "github.com/milindpandav/flogo-extensions/sse/activity", 
    "settings": {
      "sseServerRef": "dashboard_sse"
    }
  }
}
```

### Default Server Support
If you only have one SSE server, you can use the default reference:
```json
{
  "settings": {
    "sseServerRef": "default"
  }
}
```


## Testing

Run the unit tests:

```bash
cd sse/activity
go test -v
```

The test suite includes:
- Input validation tests
- Target parsing and auto-building tests
- Data formatting validation
- Event creation tests
- Server registry integration tests
- Error handling scenarios

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.
