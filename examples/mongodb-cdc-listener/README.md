# MongoDB CDC Listener — Demo App

A minimal Flogo app that demonstrates the
[MongoDB CDC Listener trigger](../../trigger/mongodb-cdc-listener/). The trigger captures document changes via
**MongoDB change streams** and fires a flow for each change; the flow logs the event with every field.

## Flow

```
MongoDB CDC Listener  ->  StartActivity (#noop)  ->  LogEvent (#log)
```

The handler maps every trigger output — `eventID`, `eventType`, `database`, `collection`, `documentKey`,
`timestamp`, `data`, `oldData`, `updatedFields`, `removedFields`, `resumeToken`, `correlationID` — into the
flow, and `LogEvent` prints them.

## Prerequisites

Change streams require a **replica set** (or sharded cluster). A single-node replica set is enough for the demo:

```bash
docker run -d --name mongo-cdc -p 27017:27017 mongo:7 --replSet rs0
docker exec mongo-cdc mongosh --quiet --eval "rs.initiate()"
```

To also receive the previous document image (`oldData`) on updates/deletes, enable pre/post images on the
collection:

```javascript
db.runCommand({ collMod: "orders", changeStreamPreAndPostImages: { enabled: true } })
```

Adjust the trigger connection settings in `mongodb-cdc-listener-demo.flogo` to match your server.

## Build

Build the executable with the Flogo CLI (`fcli`). Use a context whose `userExtensionsDir` points at this
repository (see `fcli list-context`, e.g. `flogo-studio-2265`):

```bash
fcli build-exe -f mongodb-cdc-listener-demo.flogo -c flogo-studio-2265 -n mongodb-cdc-demo -o build
```

## Run

```bash
./build/mongodb-cdc-demo
```

Then make a change and watch it captured:

```javascript
use demodb
db.orders.insertOne({ _id: 1, item: "widget", qty: 5 })
db.orders.updateOne({ _id: 1 }, { $set: { qty: 7 } })
db.orders.deleteOne({ _id: 1 })
```

Expected log output (one line per captured change):

```
MongoDB CDC Listener event: <id> | INSERT | demodb | orders | {"_id":1} | <ts> | {"_id":1,"item":"widget","qty":5} | ...
MongoDB CDC Listener event: <id> | UPDATE | demodb | orders | {"_id":1} | <ts> | ... | updatedFields={"qty":7} | ...
MongoDB CDC Listener event: <id> | DELETE | demodb | orders | {"_id":1} | <ts> | ...
```

See the [trigger README](../../trigger/mongodb-cdc-listener/) for the full configuration reference.
