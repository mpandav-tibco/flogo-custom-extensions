# SSE Trigger

A Server-Sent Events (SSE) trigger for Flogo that creates an HTTP server to stream real-time events to connected web clients.

## Overview

The SSE Trigger establishes an HTTP server that maintains persistent connections with web clients and enables real-time data streaming. It handles client connections, manages event buffering, and provides features like CORS support, connection monitoring, and automatic event replay.

## Features

### Core SSE Features
- **Real-time Event Streaming**: Push events to connected clients instantly
- **Automatic Connection Metadata**: Extracts client IP, headers, query params, and more
- **Automatic Reconnection**: Built-in browser reconnection with last-event-ID support
- **Event Types**: Support for custom event types and structured data
- **Event IDs**: Unique identification for proper event ordering and replay
- **Keep-Alive**: Configurable heartbeat to maintain connections

### Connection Management
- **Event Buffering**: Configurable event history for replay on reconnection
- **Topic Support**: Topic-based event filtering and delivery
- **CORS Support**: Cross-origin resource sharing for web applications
- **Connection Limits**: Configurable maximum concurrent connections with graceful rejection
- **Graceful Shutdown**: Clean connection termination
- **Health & Metrics**: Built-in monitoring endpoints


## Configuration
1. The SSE trigger is part of the unified SSE connector package:
2. The trigger automatically registers itself in the shared registry for integration with the SSE Send Activity.

### Trigger Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| port | integer | yes | 9998 | HTTP server port for SSE connections |
| path | string | yes | "/events" | Base path for SSE endpoint |
| maxConnections | integer | no | 1000 | Maximum concurrent connections |
| enableCORS | boolean | no | true | Enable Cross-Origin Resource Sharing |
| corsOrigins | string | no | "*" | Allowed origins (comma-separated or *) |
| keepAliveInterval | integer | no | 30 | Keep-alive interval in seconds |
| enableEventStore | boolean | no | true | Enable event buffering for replay |
| eventStoreSize | integer | no | 100 | Maximum events to store |
| eventTTL | integer | no | 3600 | Event time-to-live in seconds |

### Handler Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| topic | string | no | "" | Event topic for basic filtering |
| eventType | string | no | "message" | Default event type |


## Exposed API Endpoints

### SSE Stream
- **GET** `/events` - Main SSE endpoint
- **Query Parameters**:
  - `topic` - Subscribe to specific topic
  - `lastEventId` - Request events since this ID

### Health Check
- **GET** `/events/health` - Health status
- **Response**: 
```json
{
  "status": "healthy",
  "activeConnections": 42
}
```

### Metrics
- **GET** `/events/metrics` - Connection metrics
- **Response**:
```json
{
  "connections": 42,
  "events": 1234,
  "bytes": 567890
}
```

## Event Format

SSE events follow the standard format:

```
id: event-123
event: notification
data: {"message": "Hello World", "timestamp": "2025-07-28T10:30:00Z"}
retry: 3000

```

## Flow Integration

The trigger outputs connection information when clients connect:

```json
{
  "connectionId": "conn_1234567890_123",
  "clientIP": "192.168.1.100",
  "userAgent": "Mozilla/5.0...",
  "headers": {"authorization": "Bearer token"},
  "queryParams": {"topic": "notifications"},
  "topic": "notifications",
  "lastEventId": "event-122",
  "timestamp": "2025-07-28T10:30:00Z"
}
```



**Important**: All trigger outputs are automatically populated from the HTTP request - you don't need to configure or provide any values. The trigger extracts connection metadata and makes it available to your flows instantly.

## Activities

### SSE Send Event Activity

Send events to connected clients using the companion SSE Send Activity:

```json
{
  "id": "send_notification",
  "ref": "github.com/milindpandav/flogo-extensions/sse/activity",
  "settings": {
    "sseServerRef": "sse_trigger"
  },
  "input": {
    "target": "all",
    "data": {
      "message": "New order received",
      "timestamp": "2025-07-29T10:30:00Z"
    },
    "format": "json",
    "eventType": "notification"
  }
}
```

The trigger and activity are integrated through a shared registry - when you name your trigger, the activity can reference it by name to send events to connected clients.

## Troubleshooting

### Common Issues

1. **Connection Drops**
   - Check network stability
   - Adjust keep-alive interval
   - Review proxy/firewall settings

2. **High Memory Usage**
   - Reduce event store size
   - Check for connection leaks
   - Monitor event payload sizes

3. **Event Replay Issues**
   - Ensure event IDs are unique
   - Check event store TTL
   - Verify last-event-ID format

### Debug Mode

Enable debug logging for detailed troubleshooting:

```json
{
  "settings": {
    "logLevel": "DEBUG"
  }
}
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.
