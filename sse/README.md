# Server-Sent Events (SSE) Connector

A Flogo extension for real-time data streaming using Server-Sent Events (SSE) protocol. This connector provides server-side SSE capabilities through a unified trigger and activity pair.

## Overview

Server-Sent Events (SSE) is a web standard that allows a server to push data to web clients in real-time over a single HTTP connection. Unlike WebSockets, SSE is unidirectional (server-to-client) and uses standard HTTP, making it simpler to implement and more firewall-friendly.



## Components

This SSE connector consists of two main components:

- **[SSE Trigger](trigger/README.md)** - Creates an SSE server that accepts client connections and manages event streaming
- **[SSE Send Activity](activity/README.md)** - Sends events to connected SSE clients through the server

## Use Cases

### 📊 **Real-time Dashboards & Analytics**
- **Stock Market Dashboards**: Live price updates, trading volumes, market indicators
- **Business Intelligence**: Real-time KPI updates, sales metrics, performance dashboards
- **IoT Monitoring**: Sensor data streams, device status, environmental metrics
- **System Monitoring**: Server metrics, application performance, log streams

### 🔔 **Live Notifications & Alerts**
- **Social Media Feeds**: New posts, likes, comments, mentions
- **E-commerce**: Order status updates, inventory changes, price alerts
- **Chat Applications**: New messages, typing indicators, user presence
- **Security Alerts**: Intrusion detection, system warnings, compliance notifications

### 📈 **Financial & Trading Systems**
- **Trading Platforms**: Real-time quotes, order book updates, trade executions
- **Risk Management**: Live risk metrics, exposure calculations, limit breaches
- **Payment Processing**: Transaction status, fraud alerts, settlement updates
- **Cryptocurrency**: Price feeds, trading volumes, blockchain events

### 🎮 **Gaming & Interactive Applications**
- **Live Sports**: Score updates, player statistics, game events
- **Online Gaming**: Player actions, leaderboards, match updates
- **Auctions**: Bid updates, time remaining, winner notifications
- **Collaborative Tools**: Document changes, user cursors, edit conflicts

### 🏢 **Enterprise Integration**
- **Workflow Updates**: Process status, approval notifications, task assignments
- **Supply Chain**: Shipment tracking, inventory levels, delivery updates
- **Customer Service**: Queue status, agent availability, ticket updates
- **Manufacturing**: Production metrics, quality alerts, equipment status

### 🌐 **Content & Media Streaming**
- **News Feeds**: Breaking news, article updates, trending topics
- **Live Events**: Commentary, score updates, audience interactions
- **Content Management**: Publication status, approval workflows, content changes
- **Broadcasting**: Live captions, viewer count, engagement metrics

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Web Clients   │    │  Flogo Flows    │    │ External Data   │
│                 │    │                 │    │    Sources      │
│  ┌───────────┐  │    │ ┌─────────────┐ │    │                 │
│  │ Browser   │  │    │ │   Timer     │ │    │ ┌─────────────┐ │
│  │  Apps     │  │    │ │   REST API  │ │    │ │  Database   │ │
│  │  Mobile   │  │    │ │   Queue     │ │    │ │     API     │ │
│  └───────────┘  │    │ │   etc...    │ │    │ │   Files     │ │
└─────────┬───────┘    │ └─────────────┘ │    │ └─────────────┘ │
          │            └─────────┬───────┘    └─────────┬───────┘
          │                      │                      │
          │ HTTP/SSE             │                      │
          │                      │                      │
    ┌─────▼──────────────────────▼──────────────────────▼─────┐
    │                 Flogo Runtime                           │
    │                                                         │
    │  ┌─────────────┐              ┌─────────────────────┐    │
    │  │ SSE Trigger │◄────────────►│ SSE Send Activity   │    │
    │  │             │  Shared      │                     │    │
    │  │ • Server    │  Registry    │ • Event Creation    │    │
    │  │ • Clients   │              │ • Target Selection  │    │
    │  │ • Topics    │              │ • Broadcasting      │    │
    │  └─────────────┘              └─────────────────────┘    │
    └─────────────────────────────────────────────────────────┘
```

## Unified Structure
```
sse/
├── registry.go          # Shared registry and interfaces  
├── trigger/             # SSE Trigger 
└── activity/            # SSE Send Activity
```

### Benefits
- **Shared interfaces**: No duplication between trigger and activity
- **Simple imports**: Both packages can import from parent sse package
- **Unified registry**: Single source of truth for server registration
- **Better integration**: Direct access between components

## Key Features

### 🚀 **Core SSE Functionality**
- Real-time event streaming to web clients
- Concurrent connection handling (configurable limit)
- Automatic client reconnection with last-event-ID support
- Configurable keep-alive intervals and connection timeouts

### 📦 **Event Management**
- Event buffering and replay for reconnections  
- Configurable event TTL and storage limits
- Multiple event types and custom data formatting
- JSON, string, and auto-format support with validation

### 🎯 **Flexible Targeting**
- Broadcast to all connected clients
- Topic-based event delivery
- Individual connection targeting
- Auto-generated targeting from activity inputs

### 🌐 **Web Integration**
- CORS configuration for cross-origin requests
- Standard EventSource API compatibility
- Clean connection management and graceful shutdown
- Thread-safe connection registry

## Quick Start

#### 1. Install the Extension


#### 2. Add SSE Trigger to Your Flow

##### SSE Trigger Settings
- **Port & Path**: Server binding configuration
- **Connection Limits**: Maximum concurrent connections with graceful rejection
- **CORS**: Cross-origin request handling for web applications
- **Event Store**: Buffering and replay capabilities for reconnection
- **Keep-Alive**: Configurable heartbeat intervals

See [SSE Trigger README](trigger/README.md) for detailed configuration options.


#### 3. Add SSE Send Activity to Send Events

Use the SSE Send activity in your flows to broadcast events:

##### SSE Send Activity Settings
- **Server Reference**: Target SSE server instance
- **Event Types**: Message categorization
- **Target Selection**: Client/topic filtering
- **Data Formatting**: JSON, string, or auto-format

See [SSE Send Activity README](activity/README.md) for detailed configuration options.

#### 4. Connect from Client

```javascript
const eventSource = new EventSource('http://localhost:9998/events');

// Listen for all events
eventSource.onmessage = function(event) {
  const data = JSON.parse(event.data);
  console.log('Received:', data);
};

// Listen for specific event types
eventSource.addEventListener('update', function(event) {
  const data = JSON.parse(event.data);
  console.log('Update received:', data);
});

// Handle connection errors
eventSource.onerror = function(event) {
  console.error('SSE connection error:', event);
};
```

## Troubleshooting

### Common Issues
- **Connection refused**: Check port and firewall settings
- **CORS errors**: Verify `enableCORS` and `corsOrigins` settings
- **Missing events**: Check event store configuration and TTL
- **High memory usage**: Adjust `eventStoreSize` and `eventTTL` settings

### Debugging
- Check Flogo logs for detailed error messages
- Use browser developer tools for client-side debugging
- Monitor server resources (memory, CPU, connections)
- Verify SSE server registration in shared registry

## Contributing

Contributions are welcome! Please read our contributing guidelines and submit pull requests for any improvements.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
