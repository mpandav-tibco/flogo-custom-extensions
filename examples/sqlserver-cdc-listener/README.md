# SQL Server CDC Listener — Demo App

A minimal Flogo app that demonstrates the
[SQL Server CDC Listener trigger](../../trigger/sqlserver-cdc-listener/). The trigger captures row-level
changes from **SQL Server CDC change tables** (LSN-cursor polling) and fires a flow for each change; the flow
logs the event with every field.

## Flow

```
SQL Server CDC Listener  ->  StartActivity (#noop)  ->  LogEvent (#log)
```

The handler maps every trigger output — `eventID`, `eventType`, `database`, `schema`, `table`, `timestamp`,
`data`, `oldData`, `lsn`, `seqVal`, `operation`, `correlationID` — into the flow, and `LogEvent` prints them.

## Prerequisites

SQL Server CDC requires the **SQL Server Agent** running, plus CDC enabled on the database and table:

```bash
docker run -d --name mssql-cdc -p 1433:1433 \
  -e ACCEPT_EULA=Y -e 'MSSQL_SA_PASSWORD=Str0ng!Passw0rd' -e MSSQL_AGENT_ENABLED=true \
  mcr.microsoft.com/mssql/server:2022-latest
```

```sql
CREATE DATABASE demodb;
USE demodb;
EXEC sys.sp_cdc_enable_db;
CREATE TABLE dbo.orders (id INT PRIMARY KEY, item NVARCHAR(50), qty INT);
EXEC sys.sp_cdc_enable_table @source_schema = 'dbo', @source_name = 'orders',
     @role_name = NULL, @supports_net_changes = 0;
```

Adjust the trigger connection settings in `sqlserver-cdc-listener-demo.flogo` to match your server.

## Build

Build the executable with the Flogo CLI (`fcli`). Use a context whose `userExtensionsDir` points at this
repository (see `fcli list-context`, e.g. `flogo-studio-2265`):

```bash
fcli build-exe -f sqlserver-cdc-listener-demo.flogo -c flogo-studio-2265 -n sqlserver-cdc-demo -o build
```

## Run

```bash
./build/sqlserver-cdc-demo
```

Then make a change and watch it captured (CDC capture is asynchronous — allow a few seconds):

```sql
INSERT INTO dbo.orders (id, item, qty) VALUES (1, 'widget', 5);
UPDATE dbo.orders SET qty = 7 WHERE id = 1;
DELETE FROM dbo.orders WHERE id = 1;
```

Expected log output (one line per captured change):

```
SQL Server CDC Listener event: <id> | INSERT | demodb | dbo | orders | <ts> | {"id":1,"item":"widget","qty":5} | null | 0x... | 0x... | 2 | <corr>
SQL Server CDC Listener event: <id> | UPDATE | demodb | dbo | orders | <ts> | {"id":1,"item":"widget","qty":7} | {"id":1,"item":"widget","qty":5} | 0x... | 0x... | 4 | <corr>
SQL Server CDC Listener event: <id> | DELETE | demodb | dbo | orders | <ts> | null | {"id":1,"item":"widget","qty":7} | 0x... | 0x... | 1 | <corr>
```

See the [trigger README](../../trigger/sqlserver-cdc-listener/) for the full configuration reference.
