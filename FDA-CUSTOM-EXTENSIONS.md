# Adding Custom Extensions to Flogo Design Assistant (FDA)

## Background

The VS Code Flogo extension launches FDA as:
```
flogodesign-cli mcp http <port> --multiFileMode true
```
No `-c` (config file) flag is passed. FDA supports a `FLOGO_DESIGN_CONFIG_FILE` environment variable that it reads at startup and **merges** with its built-in config. This is the mechanism used to register custom activities, triggers, connections, and contributions with FDA.

---

## Step 1 — Understand the FDA Config Format

Export the built-in config to understand the structure:
```bash
cd /.vscode/extensions/tibco.flogo-2.26.3-2442/bin
./flogodesign-cli edc    # writes flogodesign-config.json to current dir
```

Top-level schema (version `FCON:1.0.0`):
```json
{
  "version": "FCON:1.0.0",
  "type": "SINGLE_FILE",
  "contributions": { ... },
  "connections":   { ... },
  "triggers":      { ... },
  "activities":    { ... }
}
```

Only include entries for YOUR extension — FDA merges this with its built-in config.

---

## Step 2 — Collect Descriptor Information per Extension

For each extension, you need:

| Field | Source |
|---|---|
| `ref` | `descriptor.json` / `trigger.json` → `"ref"` field |
| `import` string | `<shortname> <ref>` (e.g. `act_foo github.com/...`) |
| `group` | display category (lowercase) |
| `alias` | array of alternate names FDA can match |
| `gptAliases` | natural language phrases Copilot/LLM will use to match this type |
| `connectorPointer` | path to the connection field in `initActDef` (activities that use a connector) |
| `initActDef` / `initTrigDef` / `initConDef` | default settings/inputs structure |

### Activity entry with connection:
```json
"act_vectordb_ragquery": {
  "name": "act_vectordb_ragquery",
  "ref": "#act_vectordb_ragquery",
  "group": "vectordb",
  "description": "...",
  "import": "act_vectordb_ragquery github.com/mpandav-tibco/flogo-extensions/vectordb/activity/ragQuery",
  "alias": ["ragQuery"],
  "gptAliases": ["vectordb rag query", "vector rag search"],
  "oldName": "tibco-vectordb-ragquery",
  "connectorPointer": "initActDef.activity.settings.connection",
  "initActDef": {
    "activity": {
      "settings": { "connection": "conn://" },
      "input":    { "queryText": "", "collectionName": "", "topK": 5 }
    }
  }
}
```

**`connectorPointer` path rules:**
- Connection in `settings`: `"initActDef.activity.settings.connection"`
- Connection in `input` (like PostgreSQL): `"initActDef.activity.input.Connection"`
- No connection: omit the `connectorPointer` field entirely

### Connection entry:
```json
"con_vectordb": {
  "name": "con_vectordb",
  "ref": "#con_vectordb",
  "group": "vectordb",
  "import": "con_vectordb github.com/mpandav-tibco/flogo-extensions/vectordb/connector",
  "alias": ["VectorDB", "vectordb"],
  "gptAliases": ["vectordb connector", "weaviate connection"],
  "oldName": "vectordb-connector",
  "initConDef": {
    "settings": { "name": "VectorDB Connector", "dbType": "weaviate", "host": "localhost", "port": 8080 }
  }
}
```

### Trigger entry:
```json
"trig_sse": {
  "name": "trig_sse",
  "ref": "#trig_sse",
  "group": "sse",
  "import": "trig_sse github.com/mpandav-tibco/flogo-custom-extensions/sse/trigger",
  "alias": ["sse", "SSETrigger"],
  "gptAliases": ["sse trigger", "server sent events trigger"],
  "oldName": "sse",
  "initTrigDef": {
    "settings": { "port": 8080, "path": "/events" }
  }
}
```

### Contribution entry (one per extension module):
```json
"vectordb": {
  "ref": "github.com/mpandav-tibco/flogo-extensions/vectordb",
  "id": "VectorDB",
  "version": "1.0.0",
  "tag": "1.0.0",
  "name": "VectorDB RAG Connector",
  "s3location": "local/VectorDB",
  "isUserExtension": true
}
```

---

## Step 3 — Write the Config Generator Script

Rather than hand-editing the JSON, maintain a Python generator script that reads from the extension descriptors and outputs the merged FDA config. This ensures the config stays in sync when extensions evolve.

Generator script: `/tmp/gen-all-extensions-config.py`  
Output: `.vscode/flogo-custom-ext-fda-config.json`

Run it any time an extension changes:
```bash
python3 /tmp/gen-all-extensions-config.py
```

> **Note:** Copy `/tmp/gen-all-extensions-config.py` to a persistent location (e.g., `.vscode/gen-fda-config.py`) so it survives reboots.

---

## Step 4 — Verify the Config Loads Correctly

Test directly using `describe-project` with `-c`:
```bash
cd /Users/milindpandav/.vscode/extensions/tibco.flogo-2.26.3-2442/bin
CONFIG=/Users/milindpandav/workspaces/vscode/mcp/.vscode/flogo-custom-ext-fda-config.json
./flogodesign-cli describe-project -f <path-to-any.flogo> -c "$CONFIG" 2>&1 | grep -E "INFO|WARN|ERROR" | head -20
```

Look for:
```
[Flogo Design] (INFO)    Loaded configuration file: /path/to/flogo-custom-ext-fda-config.json
```

No `WARN: unknown activity type` lines means all refs resolved correctly.

---

## Step 5 — Set the Environment Variable

Add to `~/.zshrc` (done once — persists across sessions):
```bash
echo 'export FLOGO_DESIGN_CONFIG_FILE="/Users/milindpandav/workspaces/vscode/mcp/.vscode/flogo-custom-ext-fda-config.json"' >> ~/.zshrc
```

Verify:
```bash
grep FLOGO_DESIGN_CONFIG_FILE ~/.zshrc
```

Currently set to:
```
export FLOGO_DESIGN_CONFIG_FILE="/Users/milindpandav/workspaces/vscode/mcp/.vscode/flogo-custom-ext-fda-config.json"
```

---

## Step 6 — Restart FDA

The VS Code terminal that runs FDA must be started **after** `~/.zshrc` is sourced so it picks up `FLOGO_DESIGN_CONFIG_FILE`.

1. Stop the running FDA server from the VS Code Flogo panel (hamburger → Stop Server)
2. Start it again — VS Code opens a new terminal, which sources `~/.zshrc` automatically
3. Confirm in the FDA terminal output:
   ```
   [Flogo Design] (INFO)    Loaded configuration file: .../flogo-custom-ext-fda-config.json
   ```

Alternatively, start manually to test:
```bash
source ~/.zshrc
cd /Users/milindpandav/.vscode/extensions/tibco.flogo-2.26.3-2442/bin
./flogodesign-cli mcp http 3333 --multiFileMode true
```

---

## Current Extension Inventory

Config file: `.vscode/flogo-custom-ext-fda-config.json`  
Generator: `/tmp/gen-all-extensions-config.py`

### Contributions (12)
| ID | Module |
|---|---|
| VectorDB | `github.com/mpandav-tibco/flogo-extensions/vectordb` |
| KafkaStream | `github.com/mpandav-tibco/flogo-extensions/kafkastream` |
| SSE | `github.com/mpandav-tibco/flogo-custom-extensions/sse` |
| AWSSignatureV4 | `github.com/mpandav-tibco/flogo-extensions/activity/awssignaturev4` |
| SOAPClient | `github.com/mpandav-tibco/flogo-custom-extensions/activity/soapclient` |
| SchemaTransform | `github.com/project-flogo/custom-extensions/activity/xsdschematransform` |
| TemplateEngine | `github.com/mpandav-tibco/flogo-custom-extensions/activity/templateengine` |
| WriteLog | `github.com/milindpandav/activity/write-log` |
| XMLFilter | `github.com/milindpandav/activity/xmlfilter` |
| PostgresListener | `github.com/mpandav/trigger/postgreslistener` |
| MySQLBinlogListener | `github.com/mpandav-tibco/flogo-custom-extensions/trigger/mysql-binlog-listener` |
| RestFireForget | `github.com/mpandav-tibco/flogo-custom-extensions/activity/rest-fire-forget` |

### Connections (1)
| Name | Connector |
|---|---|
| con_vectordb | VectorDB (Weaviate/Qdrant/Chroma/Milvus) |

### Triggers (7)
| Name | Type |
|---|---|
| trig_kafkastream_filter | KafkaStream Filter |
| trig_kafkastream_aggregate | KafkaStream Aggregate |
| trig_kafkastream_join | KafkaStream Join |
| trig_kafkastream_split | KafkaStream Split |
| trig_sse | Server-Sent Events |
| trig_postgreslistener | PostgreSQL LISTEN/NOTIFY |
| trig_mysql_binlog | MySQL Binlog CDC |

### Activities (26)
| Group | Activities |
|---|---|
| vectordb | ragQuery, ingestDocuments, createCollection, deleteCollection, listCollections, vectorSearch, hybridSearch, upsertDocuments, deleteDocuments, getDocument, scrollDocuments, countDocuments, createEmbeddings, rerank |
| kafkastream | kafka-stream-aggregate, kafka-stream-filter |
| sse | sse-send |
| aws | awssignaturev4 |
| soap | soapclient |
| schematransform | xsdschematransform, jsonschematransform, avroschematransform |
| templateengine | tibco-template-engine |
| logging | tibco-write-log |
| xml | xmlfilter |
| http | rest-fire-forget |

---

## Adding a New Extension in Future

1. Locate the extension's `descriptor.json` / `activity.json` / `trigger.json`
2. Note its `ref` and `go.mod` module path
3. Add a new entry to `/tmp/gen-all-extensions-config.py`:
   - `contributions` section (one entry per module)
   - `connections` section (if it has a connector)
   - `triggers` section (if it has triggers)
   - `activities` section (one entry per activity)
4. Run `python3 /tmp/gen-all-extensions-config.py`
5. Restart FDA from the VS Code Flogo panel

---

## Troubleshooting

| Problem | Fix |
|---|---|
| FDA doesn't load config | Check `echo $FLOGO_DESIGN_CONFIG_FILE` in the FDA terminal — if empty, the terminal was opened before `~/.zshrc` was updated. Restart VS Code or the terminal. |
| `WARN: unknown activity type #foo` | The activity's `ref` in the `.flogo` file doesn't match the `import` in the config. Verify both use the same Go module path. |
| `connectorPointer` not working | Check whether connection is in `settings` vs `input` in the activity's `descriptor.json`. Use `initActDef.activity.settings.connection` for settings-based, `initActDef.activity.input.Connection` for input-based. |
| `json.tool` shows no `ref` field | The descriptor uses `go.mod` module path as the ref. Use `go.mod module` line as the ref value. |
