# Rule Engine Activity

Evaluates declarative YAML rules against structured documents (`.flogo`, `.bwp`, Kubernetes YAML, config files, log files) and returns structured findings, severity counts, and a markdown report.

Supports:
- YAML-driven rules — no code required; rules are externalised files loaded at runtime
- Five built-in parsers — JSON/Flogo (JSONPath), XML/BWP (XPath), YAML/Helm (dot-path), Key-Value (`.properties`/`.env`), and plain-text log lines
- 20 match types — existence, equality, string, regex, numeric, composite, and security-specific extended types
- Flexible scope targeting — full JSONPath (including nested wildcards), XPath, or dot-path to narrow rules to specific sub-objects
- Tag and disable filters — run only a tagged subset of rules, or skip specific rule IDs at call time
- Go template interpolation — `{{.Scope.*}}`, `{{.Root.*}}`, `{{.File.*}}`, `{{.Match}}` in descriptions and locations

---

## Inputs

| Input | Type | Required | Description |
|-------|------|----------|-------------|
| `content` | string | Yes | Raw file content to analyse |
| `fileName` | string | Yes | Original filename — drives parser auto-detection and template context |
| `rulesPath` | string | Yes | Directory of `.yaml` rule files; searched recursively |
| `parserOverride` | string | | Force a parser: `json`, `xml`, `yaml`, `kv`, `lines` |
| `disabledRules` | array | | Rule IDs to skip for this call (e.g. `["FLOGO-001"]`) |
| `tags` | array | | Only evaluate rules that carry at least one of these tags |

---

## Outputs

| Output | Type | Description |
|--------|------|-------------|
| `findings` | array | Issues found — each item has `rule_id`, `severity`, `title`, `location`, `message`, `recommendation`, `category`, `tags` |
| `positives` | array | Rules with `severity: GOOD` that matched |
| `errorCount` | integer | Number of ERROR findings |
| `warningCount` | integer | Number of WARNING findings |
| `infoCount` | integer | Number of INFO findings |
| `markdown` | string | Pre-formatted markdown analysis report |
| `overview` | object | File metadata: `file`, `extension`, `parser`, `rules_run`, `name`, `version`, `warnings` |
| `success` | boolean | `false` when a fatal error prevented evaluation |
| `error` | string | Error message when `success` is `false` |

---

## Rule Schema

Each rule is a `.yaml` file anywhere under `rulesPath`. Minimum viable rule requires `id`, `severity`, `title`, and `match`.

```yaml
rule:
  # Required
  id: RULE-001                        # Unique. Duplicate IDs are warned and skipped.
  severity: ERROR                     # ERROR | WARNING | INFO | GOOD
  title: "Short display title"
  match:                              # Main condition — see Match Types below
    type: missing
    path: "errorHandler.tasks"

  # Targeting
  scope: "$.resources[*].data"       # Expand to sub-objects. JSON: JSONPath, XML: XPath, YAML/KV: dot-path
  applies_to: [.json, .flogo]        # Limit to file extensions. Omit to match all formats.
  parser: yaml                        # Override parser for this rule only.

  # Filtering
  enabled: true                       # Set false to permanently skip (default: true)
  tags: [flogo, reliability]          # Used with the `tags` input filter

  # Optional pre-filter — evaluated before match
  when:
    type: contains
    path: "activity.ref"
    substring: "rest"

  # Templates — support {{.Scope.*}}, {{.Root.*}}, {{.File.*}}, {{.Match}}
  description: "Flow {{.Scope.name}} has no error handler."
  location: "flow:{{.Scope.name}}"
  recommendation: "Add an errorHandler section."

  # Log-analysis fields
  root_causes: ["Connection pool exhausted"]
  fixes: ["Increase pool size in ems.conf"]
```

---

## Match Types

### Existence

| Type | Matches when |
|------|-------------|
| `missing` | Path absent, null, empty string, or empty array |
| `exists` | Path present and non-empty |
| `all_missing` | Every path in `paths` (list) is absent or empty |

### Equality

| Type | Field | Matches when |
|------|-------|-------------|
| `equals` | `value` | Deep-equal or string match |
| `not_equals` | `value` | Does not match |

### String

| Type | Field | Matches when |
|------|-------|-------------|
| `contains` | `substring` | Value contains the substring |
| `not_contains` | `substring` | Value does not contain the substring |
| `contains_any` | `substrings` | Value contains at least one entry in the list |
| `regex` | `pattern`, `flags` | Value matches the regex pattern (`flags: [i]` for case-insensitive) |
| `regex_not_match` | `pattern`, `requires_contains` | Value does **not** match the pattern. If `requires_contains` is set, the rule is skipped (no finding) unless the value first contains that string — useful to avoid false positives on unrelated values. |

### Numeric

| Type | Field | Matches when |
|------|-------|-------------|
| `greater_than` | `value` | Numeric value > threshold |
| `less_than` | `value` | Numeric value < threshold |
| `count_exceeds` | `value` | Array length > threshold |
| `count_greater_than` | `min_count` | Array length > `min_count` (alias of `count_exceeds` with a named field) |

### Composite (recursive)

| Type | Field | Matches when |
|------|-------|-------------|
| `any_of` | `conditions` | At least one sub-condition matches |
| `all_of` | `conditions` | All sub-conditions match |
| `none_of` | `conditions` | No sub-condition matches |
| `none_contain` | `path` + `keys` **or** `substrings` | Object has none of the given keys (key-based), or no array element contains any of the given substrings (substring-based) |

### Security

| Type | Field | Matches when |
|------|-------|-------------|
| `credential_header_literal` | `path`, `header_names` | Any header in `header_names` found at `path` has a hard-coded literal value (i.e. not a Flogo `=$` expression). The finding value is `header: [REDACTED]` — the secret itself is never surfaced. |
| `duplicate_values` | `path`, `min_count` | Array at `path` contains the same value ≥ `min_count` times (default 2). Useful for detecting duplicate rule IDs, duplicate activity refs, etc. |

### Aliases

The following type names are accepted as readable synonyms:

| Alias | Resolves to |
|-------|------------|
| `not_empty`, `not_missing` | `exists` |
| `regex_match` | `regex` |

---

## Parsers

| Parser | Extensions | Scope syntax | Path syntax in `match` |
|--------|------------|-------------|------------------------|
| `json` | `.json`, `.flogo` | JSONPath: `$.resources[*].data` | Dot-notation: `activity.ref` |
| `xml` | `.xml`, `.bwp` | XPath: `//process[@enabled='true']` | XPath from node: `@name`, `status/text()` |
| `yaml` | `.yaml`, `.yml` | Dot-path: `spec.template.spec.containers` | Dot-path: `image` |
| `kv` | `.conf`, `.properties`, `.env` | Empty (whole document) | Key: `server.url` |
| `lines` | `.log`, `.txt`, `.out` | Empty or `$[*]` | `line` (text), `number` (1-based int) |

Auto-detection is based on file extension. Use `parserOverride` or the rule's `parser` field to override.

---

## Template Variables

Available in `description`, `location`, and `recommendation` fields. Missing fields render as empty string.

| Variable | Description |
|----------|-------------|
| `{{.Scope.<field>}}` | Field from the current scope item |
| `{{.Root.<field>}}` | Field from the document root |
| `{{.File.Name}}` | Base filename, e.g. `myapp.flogo` |
| `{{.File.Extension}}` | Lowercase extension, e.g. `.flogo` |
| `{{.Match}}` | The value that triggered the match |

---

## Examples

### Flogo — missing error handler

| Input | Value |
|-------|-------|
| `fileName` | `myapp.flogo` |
| `rulesPath` | `/rules/flogo` |
| `tags` | `["reliability"]` |

```yaml
rule:
  id: FLOGO-001
  severity: ERROR
  title: Flow Missing Error Handler
  description: "Flow {{.Scope.name}} has no error handler tasks."
  tags: [flogo, reliability]
  applies_to: [.json, .flogo]
  scope: "$.resources[*].data"
  match:
    type: missing
    path: "errorHandler.tasks"
  location: "flow:{{.Scope.name}}"
  recommendation: "Add an errorHandler section with at least one task."
```

### Kubernetes — unpinned container image

| Input | Value |
|-------|-------|
| `fileName` | `deployment.yaml` |
| `rulesPath` | `/rules/kube` |

```yaml
rule:
  id: KUBE-001
  severity: ERROR
  title: Container Uses latest or Untagged Image
  description: "Container {{.Scope.name}} uses image {{.Match}} with no pinned version."
  tags: [kubernetes, deployment]
  applies_to: [.yaml, .yml]
  scope: "spec.template.spec.containers"
  match:
    type: any_of
    conditions:
      - type: regex
        path: image
        pattern: ":latest$"
      - type: not_contains
        path: image
        substring: ":"
  location: "container:{{.Scope.name}}"
  recommendation: "Pin the image to a specific digest or version tag."
```

### Log file — error/fatal entries

| Input | Value |
|-------|-------|
| `fileName` | `app.log` |
| `rulesPath` | `/rules/ops` |

```yaml
rule:
  id: LOG-001
  severity: ERROR
  title: Error or Fatal Log Entry
  description: "Line {{.Scope.number}}: {{.Scope.line}}"
  tags: [logs, operations]
  applies_to: [.log, .txt]
  match:
    type: regex
    path: line
    pattern: "ERROR|FATAL|PANIC"
    flags: [i]
  location: "line:{{.Scope.number}}"
  root_causes: ["Application exception", "Unhandled panic"]
  fixes: ["Check stack trace in surrounding lines"]
```

---

## Limitations

| Area | Limitation |
|------|------------|
| **XML path resolution** | Scope items are `*xmlquery.Node`; match paths must be valid XPath from that node. Dot-notation does not apply to XML. |
| **Rule hot-reload** | Rules are reloaded from disk on every `Evaluate` call. This is intentional but adds I/O for large rule sets. |
| **Expression match type** | `type: expression` (JavaScript sandbox) is defined in the schema but not yet implemented — returns an error if used. |
| **Concurrent rule modification** | No file-watch or lock mechanism. Modifying rule files during evaluation may produce inconsistent results. |

---

## Running Tests

```bash
cd activity/ruleengine

# Unit and engine tests (no external services required)
go test ./engine/... -v

# All packages
go test ./... -count=1

# With race detector
go test ./engine/... -race
```

Integration tests use a local HTTP server — see `integration/cmd/server/main.go`. Required because `flogobuild` cannot resolve sub-module replace directives.

```bash
cd activity/ruleengine/integration
go run ./cmd/server        # starts on :7000
```
