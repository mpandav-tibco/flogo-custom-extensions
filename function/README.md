# Custom Flogo Functions

Custom Flogo expression functions extending the built-in set — math, array, string, and utility helpers.

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
