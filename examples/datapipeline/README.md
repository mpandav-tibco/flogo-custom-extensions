# Run Data Pipeline Demo

A REST-triggered flow that runs a pipeline of a [Flogo Data Pipeline](https://github.com/mpandav-tibco/flogo-datapipeline) engine with the [Run Data Pipeline](../../activity/datapipeline/) activity, waits for the pipeline's result, and replies with it.

```
GET :9999/run  →  RunDataPipeline (filter-and-map, wait, overlap: queue)  →  Return {runId, status, rowsProcessed, rowsWritten, rowsDropped, durationMs}
```

## Run it

1. Start the engine on the data pipeline repository's `filter-and-map` example, with its API on port 9080:

   ```bash
   cd flogo-datapipeline/examples/02-filter-and-map
   flogo-datapipeline -f filter-and-map.datapipeline -api-addr 127.0.0.1:9080
   ```

2. Open `run-data-pipeline-demo.flogo` in VS Code with this repository's `activity/datapipeline` registered as a user extension, then build and run the app. Its app properties are `DataPipeline.EngineURL` (default `http://localhost:9080`) and `DataPipeline.Token` (empty; set it to the engine's `DP_API_TOKEN` for a deployed engine).

3. Call the flow:

   ```bash
   curl localhost:9999/run
   ```
   ```json
   { "runId": "c943c84eed4b295b", "status": "completed", "rowsProcessed": 30, "rowsWritten": 20, "rowsDropped": 10, "durationMs": 2 }
   ```

The flow passes `overlap: queue`, so two calls at once run one after the other rather than together. With `FLOGO_OTEL_TRACE=true` on both the app and the engine, the engine's run is a child span of the flow's activity, in the same trace.
