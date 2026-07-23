# PostgreSQL CDC Listener — Demo App

A minimal Flogo app that demonstrates the
[PostgreSQL CDC Listener trigger](../../trigger/postgres-cdc-listener/). The trigger streams row-level
`INSERT`/`UPDATE`/`DELETE` changes from the PostgreSQL write-ahead log via **logical replication** and fires a
flow for each change; the flow logs the event with every field.

## Flow

```
PostgreSQL CDC Listener  ->  StartActivity (#noop)  ->  LogEvent (#log)
```

The handler maps every trigger output — `eventID`, `eventType`, `database`, `schema`, `table`, `timestamp`,
`data`, `oldData`, `lsn`, `xid`, `correlationID` — into the flow, and `LogEvent` prints them.

## Prerequisites

A PostgreSQL server with logical replication enabled and a user holding the `REPLICATION` attribute:

```bash
docker run -d --name pg-cdc -p 5432:5432 \
  -e POSTGRES_USER=cdc_user -e POSTGRES_PASSWORD=cdcpass -e POSTGRES_DB=demodb \
  postgres:16 -c wal_level=logical -c max_replication_slots=10 -c max_wal_senders=10
```

Adjust the trigger connection settings in `postgres-cdc-listener-demo.flogo` to match your server.

## Build

Build the executable with the Flogo CLI (`fcli`). Use a context whose `userExtensionsDir` points at this
repository (see `fcli list-context`, e.g. `flogo-studio-2265`):

```bash
fcli build-exe -f postgres-cdc-listener-demo.flogo -c flogo-studio-2265 -n postgres-cdc-demo -o build
```

## Run

```bash
./build/postgres-cdc-demo
```

On start the trigger auto-creates the publication and replication slot and begins streaming. Make a change:

```sql
CREATE TABLE demo_items(id serial primary key, name text, qty int);
ALTER TABLE demo_items REPLICA IDENTITY FULL;   -- full before-image on UPDATE/DELETE
INSERT INTO demo_items(name, qty) VALUES ('widget', 5);
UPDATE demo_items SET qty = 7 WHERE name = 'widget';
DELETE FROM demo_items WHERE name = 'widget';
```

Expected log output (one line per captured change):

```
PostgreSQL CDC Listener event: <id> | INSERT | demodb | public | demo_items | <ts> | {"id":1,"name":"widget","qty":5} | null      | 0/... | <xid> | <corr>
PostgreSQL CDC Listener event: <id> | UPDATE | demodb | public | demo_items | <ts> | {"id":1,"name":"widget","qty":7} | {"id":1,"name":"widget","qty":5} | 0/... | <xid> | <corr>
PostgreSQL CDC Listener event: <id> | DELETE | demodb | public | demo_items | <ts> | null      | {"id":1,"name":"widget","qty":7} | 0/... | <xid> | <corr>
```

See the [trigger README](../../trigger/postgres-cdc-listener/) for the full configuration reference.
