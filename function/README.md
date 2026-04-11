# Custom Flogo Functions

Custom Flogo expression functions extending the built-in set — math, array, string, utility, and datetime helpers.

## Functions

### `math` — Math helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `math.abs` | `abs(x float64) float64` | Absolute value |
| `math.pow` | `pow(base, exp float64) float64` | Raises `base` to the power of `exp` |
| `math.sqrt` | `sqrt(x float64) float64` | Square root; error if x < 0 |
| `math.log` | `log(x float64) float64` | Natural logarithm (base e); error if x ≤ 0 |
| `math.log2` | `log2(x float64) float64` | Base-2 logarithm; error if x ≤ 0 |
| `math.log10` | `log10(x float64) float64` | Base-10 logarithm; error if x ≤ 0 |
| `math.sign` | `sign(x float64) float64` | Returns -1, 0, or 1 |
| `math.clamp` | `clamp(val, min, max float64) float64` | Bounds val to [min, max] |

### `array` — Array helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `array.min` | `min(arr) float64` | Minimum numeric value in an array |
| `array.max` | `max(arr) float64` | Maximum numeric value in an array |
| `array.avg` | `avg(arr) float64` | Average (mean) of numeric values in an array |
| `array.unique` | `unique(arr) []interface{}` | Deduplicated array preserving original order |
| `array.indexOf` | `indexOf(arr, value) int` | Index of first occurrence; `-1` if not found |
| `array.sort` | `sort(arr) []interface{}` | New array sorted ascending (numeric or string) |
| `array.sortDesc` | `sortDesc(arr) []interface{}` | New array sorted descending |
| `array.first` | `first(arr) interface{}` | First element; `nil` for empty array |
| `array.last` | `last(arr) interface{}` | Last element; `nil` for empty array |
| `array.sumBy` | `sumBy(arr, field string) float64` | Sum a numeric field across an array of objects |

### `string` — String helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `string.padLeft` | `padLeft(str string, size int, padChar string) string` | Left-pad `str` with `padChar` to `size` characters |
| `string.padRight` | `padRight(str string, size int, padChar string) string` | Right-pad `str` with `padChar` to `size` characters |
| `string.mask` | `mask(str string, keepFirst int, keepLast int) string` | Replace middle characters with `*` |
| `string.truncate` | `truncate(str string, maxLen int) string` | Truncate to `maxLen` chars, appending `...` if cut |
| `string.isBlank` | `isBlank(str string) bool` | True if nil, empty, or whitespace-only |
| `string.isNumeric` | `isNumeric(str string) bool` | True if string is a valid decimal integer or float |
| `string.camelCase` | `camelCase(str string) string` | Convert to lowerCamelCase |
| `string.snakeCase` | `snakeCase(str string) string` | Convert to snake_case |

### `util` — Utility helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `util.coalesce` | `coalesce(vals ...interface{}) interface{}` | Returns the first non-nil, non-empty value |
| `util.sha256` | `sha256(str string) string` | SHA-256 hex digest (64-char lowercase) |

### `datetime` — Datetime helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `datetime.isBefore` | `isBefore(d1, d2 string) bool` | True if d1 is strictly before d2 |
| `datetime.isAfter` | `isAfter(d1, d2 string) bool` | True if d1 is strictly after d2 |
| `datetime.toEpoch` | `toEpoch(d string) int64` | Datetime string → Unix epoch milliseconds |
| `datetime.fromEpoch` | `fromEpoch(ms int64) string` | Unix epoch milliseconds → RFC3339 UTC string |
| `datetime.isWeekend` | `isWeekend(d string) bool` | True if date is Saturday or Sunday |
| `datetime.isWeekday` | `isWeekday(d string) bool` | True if date is Monday through Friday |

> All datetime functions accept any format supported by Flogo's `coerce.ToDateTime` (RFC3339, ISO 8601, `2006-01-02`, etc.).

---

## Debug Logging

All functions emit `Debugf` log lines via `log.RootLogger()` (from `github.com/project-flogo/core/support/log`) at key points:
- **Entry**: input parameters
- **Intermediate**: key computed values (e.g. running sum, word split results)
- **Exit**: final result

Set `FLOGO_LOG_LEVEL=DEBUG` to activate.

---

## Usage in a Flogo App

Add the root package as a single import in your `.flogo` file:

```json
"imports": [
  "github.com/milindpandav/flogo-extensions/function"
]
```

The root `all.go` blank-imports all sub-packages, triggering their `init()` functions and registering every custom function — the same pattern used by TIBCO's built-in `wi-contrib/function` package.

Use the functions in mapper expressions:

```
=math.sqrt(9)
=math.clamp($flow.temperature, -40, 120)
=math.sign($flow.balance)

=array.sort($activity[Source].output.readings)
=array.first($activity[Source].output.items)
=array.sumBy($activity[Source].output.orders, "amount")

=string.truncate($flow.description, 100)
=string.isBlank($flow.optionalField)
=string.camelCase($flow.fieldName)
=string.snakeCase($flow.headerName)

=datetime.isBefore($flow.startDate, $flow.endDate)
=datetime.toEpoch($flow.eventTime)
=datetime.fromEpoch($flow.timestampMs)
=datetime.isWeekday($flow.scheduledDate)
```

---

## Module

```
module: github.com/milindpandav/flogo-extensions/function
go:     1.21
```

---

## Tests

```bash
cd function
go test ./...
```

207 unit tests across all packages — all passing.

---

## Example App

[`examples/functions/validate-custom-functions.flogo`](../examples/functions/validate-custom-functions.flogo) — timer-triggered flow that exercises all custom functions with real-world inputs and logs the combined result.


## Functions

### `math` — Math helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `math.abs` | `abs(x float64) float64` | Absolute value |
| `math.pow` | `pow(base, exp float64) float64` | Raises `base` to the power of `exp` |

### `array` — Array helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `array.min` | `min(arr) float64` | Minimum numeric value in an array |
| `array.max` | `max(arr) float64` | Maximum numeric value in an array |
| `array.avg` | `avg(arr) float64` | Average (mean) of numeric values in an array |
| `array.unique` | `unique(arr) []interface{}` | Deduplicated array preserving original order |
| `array.indexOf` | `indexOf(arr, value) int` | Index of first occurrence; `-1` if not found |

### `string` — String helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `string.padLeft` | `padLeft(str string, size int, padChar string) string` | Left-pad `str` with `padChar` to `size` characters |
| `string.padRight` | `padRight(str string, size int, padChar string) string` | Right-pad `str` with `padChar` to `size` characters |
| `string.mask` | `mask(str string, keepFirst int, keepLast int) string` | Replace middle characters with `*`, keeping `keepFirst` and `keepLast` chars visible |

### `util` — Utility helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `util.coalesce` | `coalesce(vals ...interface{}) interface{}` | Returns the first non-nil, non-empty value |
| `util.sha256` | `sha256(str string) string` | SHA-256 hex digest (64-char lowercase) |

---

## Usage in a Flogo App

Add the root package as a single import in your `.flogo` file:

```json
"imports": [
  "github.com/milindpandav/flogo-extensions/function"
]
```

The root `all.go` blank-imports all sub-packages, triggering their `init()` functions and registering every custom function with the Flogo engine — the same pattern used by TIBCO's built-in `wi-contrib/function` package.

Then use the functions directly in mapper expressions:

```
=math.abs($activity[Source].output.temperature)
=math.pow(2, 10)

=array.min($activity[Source].output.readings)
=array.max($activity[Source].output.readings)
=array.avg($activity[Source].output.readings)
=array.unique($activity[Source].output.deviceIds)
=array.indexOf($activity[Source].output.statuses, "active")

=string.padLeft($activity[Source].output.orderId, 8, "0")
=string.padRight($activity[Source].output.currency, 10, " ")
=string.mask($activity[Source].output.cardNumber, 0, 4)

=util.coalesce($activity[Source].output.region, $env[DEFAULT_REGION], "us-east-1")
=util.sha256($activity[Source].output.invoiceId)
```

---

## Module

```
module: github.com/milindpandav/flogo-extensions/function
go:     1.21
```

---

## Tests

```bash
cd function
go test ./...
```

74 unit tests across all packages — all passing.

---

## Example App

[`examples/functions/validate-custom-functions.flogo`](../examples/functions/validate-custom-functions.flogo) — timer-triggered flow that exercises all 11 functions with real-world inputs (temperatures, card/email masking, order IDs, device arrays, SHA-256 hashing) and logs the combined result.
