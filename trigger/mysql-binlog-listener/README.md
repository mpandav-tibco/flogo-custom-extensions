# MySQL Binlog Listener Trigger

A Flogo trigger that captures real-time MySQL and MariaDB database changes using binlog streaming for event-driven architectures and change data capture (CDC) scenarios.

## Overview

This trigger provides a robust solution for real-time MySQL/MariaDB binlog monitoring including SSL/TLS support, connection management, schema information, enhanced logging, and distributed tracing capabilities.

## Features

- **📡 Real-time Binlog Streaming**: Continuous monitoring of MySQL/MariaDB binary logs with sub-second latency
- **🎯 Multi-table Support**: Monitor specific tables or entire databases with flexible filtering
- **🔍 Event Type Filtering**: Selective monitoring of INSERT, UPDATE, and DELETE operations
- **🏷️ Schema Enhancement**: Rich column names and type information instead of generic column indices
- **🔒 SSL/TLS Support**: Full SSL/TLS encryption support with multiple modes (disable, require, verify-ca, verify-full) for secure database connections
- **🔄 Connection Management**: Built-in connection retry logic with health monitoring and graceful recovery
- **🛑 Graceful Shutdown**: Proper resource cleanup and graceful shutdown handling
- **📋 Enhanced Logging**: Efficient debugging with correlation IDs and structured logging
- **🌐 OpenTelemetry Integration**: Full distributed tracing support with rich MySQL-specific metadata
- **⚡ Health Monitoring**: Periodic connection health checks with detailed metrics
- **🧠 Memory Tracking**: Runtime memory usage monitoring and resource statistics
- **⏮️ Position Control**: Resume from specific binlog positions for fault-tolerant processing
- **🏃 High Performance**: Direct binlog streaming without polling overhead for zero data loss

## Configuration

### Trigger Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `host` | string | ✅ Yes | `localhost` | MySQL/MariaDB server host |
| `port` | integer | ✅ Yes | `3306` | MySQL/MariaDB server port |
| `user` | string | ✅ Yes | `root` | Database user with REPLICATION privileges |
| `password` | string | ✅ Yes | `password` | Database password |
| `databaseName` | string | ✅ Yes | `mysql` | Target database name to monitor |
| `sslMode` | string | No | `disable` | SSL mode (`disable`, `require`, `verify-ca`, `verify-full`) |
| `sslCert` | string | No | - | SSL client certificate file path |
| `sslKey` | string | No | - | SSL client private key file path |
| `sslCA` | string | No | - | SSL CA certificate file path |
| `skipSSLVerify` | boolean | No | `false` | Skip SSL certificate verification |
| `connectionTimeout` | string | No | `30s` | Connection timeout duration |
| `readTimeout` | string | No | `30s` | Read timeout duration |
| `maxRetryAttempts` | string | No | `3` | Maximum connection retry attempts |
| `retryDelay` | string | No | `5s` | Delay between retry attempts |
| `healthCheckInterval` | string | No | `60s` | Health check frequency |
| `enableHeartbeat` | boolean | No | `true` | Enable heartbeat for connection monitoring |
| `heartbeatInterval` | string | No | `30s` | Heartbeat interval |

### Handler Settings

| Setting | Type | Required | Default | Description |
|---------|------|----------|---------|-------------|
| `serverID` | integer | ✅ Yes | `1001` | Unique MySQL server ID (1001-4999 recommended) |
| `binlogFile` | string | No | - | Starting binlog file (uses current if empty) |
| `binlogPos` | integer | No | `4` | Starting binlog position |
| `tables` | array | No | `[]` | Tables to monitor (empty = all tables) |
| `eventTypes` | string | No | `ALL` | Event types to capture (`ALL`, `INSERT`, `UPDATE`, `DELETE`) |
| `includeSchema` | boolean | No | `false` | **Include schema information (recommended)** |
| `maxRetries` | integer | No | `3` | Maximum retry attempts for failed operations |
| `retryDelay` | string | No | `5s` | Delay between retry attempts |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `eventID` | string | Unique event identifier |
| `eventType` | string | Type of database event (INSERT, UPDATE, DELETE) |
| `database` | string | Database name |
| `table` | string | Table name |
| `timestamp` | string | Event timestamp in RFC3339 format |
| `data` | object | Event data containing row information |
| `schema` | object | Schema information (when includeSchema=true) |
| `binlogFile` | string | Binlog file name where event occurred |
| `binlogPos` | integer | Binlog position of the event |
| `serverID` | integer | MySQL server ID that generated the event |
| `gtid` | string | Global Transaction ID (if GTID enabled) |
| `correlationID` | string | Correlation ID for tracking related events |

## MySQL/MariaDB Setup

### Enable Binary Logging

```sql
-- Add to my.cnf or my.ini
[mysqld]
log-bin=mysql-bin
server-id=1
binlog-format=ROW
```

### Create Replication User

```sql
-- Create replication user
CREATE USER 'repl_user'@'%' IDENTIFIED BY 'secure_password';

-- Grant required privileges
GRANT REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO 'repl_user'@'%';
GRANT SELECT ON your_target_database.* TO 'repl_user'@'%';

-- Apply changes
FLUSH PRIVILEGES;
```

**Important**: The trigger requires three distinct privileges:
- `REPLICATION SLAVE`: Read binary log events
- `REPLICATION CLIENT`: Execute binlog status commands
- `SELECT`: Access target database tables (for schema information)

### Test Configuration

```sql
-- Check MySQL version (auto-detected by trigger)
SELECT VERSION();

-- Check binary logging status
SHOW VARIABLES LIKE 'log_bin%';

-- View current binlog position (version-dependent)
-- For MySQL 8.0.22+
SHOW BINARY LOG STATUS;
-- For MySQL 5.7 and earlier
SHOW MASTER STATUS;

-- Check user privileges
SHOW GRANTS FOR 'repl_user'@'%';

-- Verify SSL is available (optional)
SHOW VARIABLES LIKE 'have_ssl';
```

### SSL Configuration

The MySQL binlog listener supports SSL/TLS configuration with multiple security modes and file-based certificate authentication.

### SSL/TLS Support
- **Complete SSL/TLS**: Supports all SSL modes (disable, require, verify-ca, verify-full) with file-based certificate authentication
- **Client Authentication**: Full mutual TLS support with client certificates for enhanced security  
- **Certificate Validation**: Comprehensive CA certificate verification and hostname validation
- **Binlog Encryption**: SSL/TLS encryption applies to both database connections and binlog streaming


#### Supported SSL Modes

| Mode | Description | Requirements |
|------|-------------|--------------|
| `disable` | No SSL/TLS encryption (default) | None |
| `require` | Require SSL/TLS encryption | None (uses server's default certificate) |
| `verify-ca` | Verify server certificate against CA | `sslCA` required |
| `verify-full` | Verify server certificate and hostname | `sslCA` required |

#### Certificate Configuration

**File-based Configuration:**
- `sslCA`: Path to CA certificate file (.pem, .crt, .cer)
- `sslCert`: Path to client certificate file (.pem, .crt, .cer)  
- `sslKey`: Path to client private key file (.pem, .key)

## Schema Feature Comparison

### Default Output (includeSchema = false)

```json
{
  "eventID": "mysql_1754067974583119000_583120000",
  "eventType": "INSERT",
  "database": "testdb",
  "table": "orders",
  "timestamp": "2025-08-01T19:06:14+02:00",
  "data": {
    "col_0": 22,
    "col_1": "CUST001",
    "col_2": "Laptop",
    "col_3": 1,
    "col_4": 1,
    "col_5": "999.99",
    "col_6": "2025-08-01 19:06:14",
    "col_7": "2025-08-01 19:06:14"
  },
  "schema": null,
  "binlogFile": "mysql-bin.000003",
  "binlogPos": 7062,
  "serverID": 1,
  "correlationID": "mysql_1754067974583126000_583126000"
}
```

### Enhanced Output (includeSchema = true)

```json
{
  "eventID": "mysql_1754069598681455000_681456000",
  "eventType": "INSERT",
  "database": "testdb",
  "table": "orders",
  "timestamp": "2025-08-01T19:33:18+02:00",
  "data": {
    "created_at": "2025-08-01 19:33:18",
    "customer_id": "CUST001",
    "id": 24,
    "modified_at": "2025-08-01 19:33:18",
    "product_name": "Laptop",
    "quantity": 1,
    "status": 1,
    "total_amount": "999.99"
  },
  "schema": {
    "database": "testdb",
    "table": "orders",
    "columns": [
      {
        "name": "id",
        "type": "int",
        "nullable": false,
        "key": "PRI",
        "extra": "auto_increment",
        "ordinal_position": 1
      },
      {
        "name": "customer_id",
        "type": "varchar",
        "nullable": false,
        "ordinal_position": 2
      }
    ]
  },
  "binlogFile": "mysql-bin.000003",
  "binlogPos": 9188,
  "serverID": 1,
  "correlationID": "mysql_1754069598681472000_681472000"
}
```

**Schema Benefits:**
- **Column Names**: Use actual database column names instead of `col_0`, `col_1` indices
- **Type Information**: Full MySQL column type metadata (varchar, int, timestamp, etc.)
- **Schema Metadata**: Column constraints, keys, defaults, and position information
- **Smart Caching**: Automatic schema caching for optimal performance
- **Fallback Support**: Graceful degradation to column indices if schema fetch fails

**Example Log Output:**
```
[INITIALIZE] Starting MySQL Binlog Listener Trigger initialization [corr_id=a1b2c3d4 trigger=mysql-binlog-a1b2c3d4 op=INITIALIZE]
[BINLOG_EVENT] Processing binlog event [corr_id=e5f6g7h8 event_type=INSERT table=orders processing_time=2.1ms]
[HEALTH_MONITOR] MySQL connection healthy - completed 10 health checks [uptime=10m0s memory_mb=67 goroutines=15 gc_runs=23]
[HEARTBEAT] MySQL binlog trigger heartbeat - trigger is alive [uptime=5m30s memory_mb=65 goroutines=15]
```

## MySQL Version Compatibility

### Current Status
This trigger **supports both MySQL 5.7/MariaDB and MySQL 8.0+** with automatic version detection and syntax adaptation.

✅ **Automatic Version Detection**: Detects MySQL/MariaDB version at startup  
✅ **Dynamic SQL Syntax**: Uses appropriate commands based on detected version  
✅ **Fallback Support**: Gracefully falls back to legacy syntax if new syntax fails  
✅ **Comprehensive Logging**: Logs version detection and SQL syntax selection  

### Version Compatibility
- **MySQL 8.0.22+**: Uses `SHOW BINARY LOG STATUS` (new syntax)
- **MySQL 5.7 and earlier**: Uses `SHOW MASTER STATUS` (legacy syntax) 
- **MariaDB (all versions)**: Uses `SHOW MASTER STATUS` (legacy syntax)


## Error Handling

The trigger provides comprehensive error handling:

- **🔄 Connection Failures**: Automatic retry mechanisms with exponential backoff
- **⚠️ Invalid Configuration**: Detailed validation with actionable error messages
- **📡 Binlog Reconnection**: Automatic reconnection on network issues with position recovery
- **📝 Error Logging**: Comprehensive error logging with troubleshooting suggestions
- **🚨 Health Alerts**: Proactive monitoring with health check warnings
- **🛡️ Schema Fallbacks**: Automatic fallback to column indices when schema fetch fails
- **🔐 SSL Connection Issues**: Graceful handling of SSL/TLS connection problems

**Common Error Examples:**
```
[INITIALIZE] Settings validation failed [error=serverID must be between 1 and 4294967295 suggestion=Check handler configuration]
[INITIALIZE] Initial MySQL connection ping failed [error=connection refused suggestion=Check database server status]
[INITIALIZE] SSL connection failed [error=x509: certificate signed by unknown authority suggestion=Verify CA certificate configuration]
[INITIALIZE] TLS handshake failed [error=tls: bad certificate suggestion=Check client certificate and key pairing]
[INITIALIZE] SSL certificate verification failed [error=x509: certificate is valid for localhost, not mysql.example.com suggestion=Use correct hostname or sslMode=verify-ca]
[BINLOG_STREAM] Schema fetch failed for table [table=orders fallback=using column indices suggestion=Check SELECT privileges]
[HEALTH_MONITOR] Health check detected issues [issues=[binlog_lag: 5000ms] suggestion=Monitor binlog processing performance]
```

## Dependencies

- **[github.com/go-mysql-org/go-mysql](https://github.com/go-mysql-org/go-mysql)** v1.12.0 - MySQL binlog parsing
- **[github.com/go-sql-driver/mysql](https://github.com/go-sql-driver/mysql)** v1.7.1 - MySQL driver
- **[github.com/project-flogo/core](https://github.com/project-flogo/core)** v1.6.13 - Flogo core framework

## How It Works

1. **Initialization**: The trigger establishes a connection to MySQL/MariaDB and validates the configuration
2. **Version Detection**: Automatically detects MySQL/MariaDB version and adapts SQL syntax accordingly
3. **Binlog Streaming**: Creates a binlog syncer that subscribes to the specified database and tables
4. **Schema Caching**: Fetches and caches table schemas from INFORMATION_SCHEMA (when includeSchema=true)
5. **Event Processing**: When a binlog event is received, it executes the associated Flogo flow with enriched data
6. **Health Monitoring**: Periodic health checks ensure connections remain active and binlog lag is acceptable
7. **Shutdown**: Graceful cleanup of all connections and resources with position preservation

## Connection Management

- **Retry Logic**: Automatic retry on connection failures with configurable attempts and delays
- **Health Monitoring**: Periodic ping and binlog lag monitoring to detect connection issues
- **Resource Cleanup**: Automatic cleanup of database connections and binlog syncers
- **Position Tracking**: Automatic binlog position advancement and recovery on restart
- **Graceful Shutdown**: Proper closure of all database connections with final position save




## Version History

- **v1.1.0**: Enhanced Schema Features & Monitoring
  - Schema information support with column names and types
  - Thread-safe schema caching for performance
  - Automatic fallback to column indices on schema errors
  - MySQL 8.0+ compatibility with automatic version detection
  - Enhanced logging with correlation IDs and structured output
  - **Health Monitoring**: Periodic connection health checks with detailed metrics
  - **Memory Tracking**: Runtime memory usage monitoring and resource statistics
  - **Heartbeat System**: Connection monitoring with configurable intervals
- **v1.0.0**: Initial release with Full SSL/TLS Support
  - Real-time binlog streaming functionality
  - **Complete SSL/TLS Implementation**: Full SSL/TLS support with all modes (disable, require, verify-ca, verify-full)
  - **Certificate-based Authentication**: Client certificate support for mutual TLS authentication
  - **SSL Configuration Validation**: Enhanced SSL configuration validation and error handling
  - Health monitoring and graceful shutdown
  - Multi-table and event type filtering

## License

This project is part of the TIBCO Flogo custom extensions collection.

## Support

For issues, questions, or contributions, please refer to the project's issue tracking system or documentation.

