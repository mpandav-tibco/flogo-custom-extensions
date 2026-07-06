"""
Minimal LanceDB REST server for local development and integration testing.

Implements the subset of the LanceDB Cloud REST API used by the Flogo
vectordb-lancedb connector:

  GET  /v1/table/                      list_tables
  GET  /v1/table/{name}/               describe table (200) or 404
  POST /v1/table/{name}/               create table (schema JSON or empty-data fallback)
  DELETE /v1/table/{name}/             drop table
  GET  /v1/table/{name}/count_rows/    count rows (returns plain integer)
  POST /v1/table/{name}/insert/        append records
  POST /v1/table/{name}/merge_insert/  upsert records (on=id by default)
  POST /v1/table/{name}/delete/        delete by SQL predicate
  POST /v1/table/{name}/query/         vector search / FTS / scan
"""

import os
import json
import pyarrow as pa
import lancedb
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import PlainTextResponse
import uvicorn

DATA_DIR = os.environ.get("LANCEDB_DATA_DIR", "/data")
os.makedirs(DATA_DIR, exist_ok=True)

app = FastAPI(title="LanceDB Local Server", version="1.0.0")

# Track which (table, column) pairs have had FTS indexes created to avoid
# rebuilding the index on every query — rebuilding is expensive and causes
# write amplification / file-lock contention under concurrent requests.
_fts_indexed: set[tuple[str, str]] = set()


# ── DB helpers ────────────────────────────────────────────────────────────────

def _db() -> lancedb.DBConnection:
    return lancedb.connect(DATA_DIR)


def _open_or_404(db: lancedb.DBConnection, name: str):
    try:
        return db.open_table(name)
    except Exception:
        raise HTTPException(status_code=404, detail=f"Table '{name}' not found")


# ── Collection management ─────────────────────────────────────────────────────

@app.get("/v1/table/")
def list_tables():
    db = _db()
    return {"tables": db.table_names()}


@app.get("/v1/table/{name}/")
def describe_table(name: str):
    db = _db()
    if name not in db.table_names():
        raise HTTPException(status_code=404, detail=f"Table '{name}' not found")
    tbl = db.open_table(name)
    return {"name": name, "rows": tbl.count_rows()}


@app.post("/v1/table/{name}/")
async def create_table(name: str, request: Request):
    db = _db()
    body = await request.json()

    if name in db.table_names():
        raise HTTPException(status_code=409, detail=f"Table '{name}' already exists")

    # Extract embedding dimensions from Arrow schema body.
    dims = _extract_dims(body)

    # Fallback: infer from inline data rows.
    if dims is None:
        for row in body.get("data", []):
            emb = row.get("embedding")
            if emb:
                dims = len(emb)
                break

    if dims is None:
        raise HTTPException(
            status_code=422,
            detail="Cannot determine embedding dimensions from request body",
        )

    schema = pa.schema([
        pa.field("id",        pa.utf8(),                       nullable=False),
        pa.field("content",   pa.utf8(),                       nullable=True),
        pa.field("metadata",  pa.utf8(),                       nullable=True),
        pa.field("embedding", pa.list_(pa.float32(), dims),    nullable=True),
    ])
    db.create_table(name, schema=schema)
    return {"name": name, "status": "created", "dimensions": dims}


@app.delete("/v1/table/{name}/")
def drop_table(name: str):
    db = _db()
    if name not in db.table_names():
        raise HTTPException(status_code=404, detail=f"Table '{name}' not found")
    db.drop_table(name)
    # Clear FTS index cache entries for the dropped table.
    to_remove = {k for k in _fts_indexed if k[0] == name}
    _fts_indexed.difference_update(to_remove)
    return {"status": "dropped"}


# ── Document operations ───────────────────────────────────────────────────────

@app.get("/v1/table/{name}/count_rows/")
def count_rows(name: str):
    db = _db()
    tbl = _open_or_404(db, name)
    return PlainTextResponse(str(tbl.count_rows()))


@app.post("/v1/table/{name}/insert/")
async def insert(name: str, request: Request):
    db = _db()
    tbl = _open_or_404(db, name)
    body = await request.json()
    records = body.get("data", [])
    if not records:
        return {"status": "ok", "inserted": 0}
    tbl.add(_to_arrow(tbl.schema, records))
    return {"status": "ok", "inserted": len(records)}


@app.post("/v1/table/{name}/merge_insert/")
async def merge_insert(name: str, request: Request):
    db = _db()
    tbl = _open_or_404(db, name)
    body = await request.json()
    records = body.get("data", [])
    on_field = body.get("on", "id")
    if not records:
        return {"status": "ok"}
    arrow = _to_arrow(tbl.schema, records)
    (
        tbl.merge_insert(on_field)
        .when_matched_update_all()
        .when_not_matched_insert_all()
        .execute(arrow)
    )
    return {"status": "ok", "upserted": len(records)}


@app.post("/v1/table/{name}/delete/")
async def delete_rows(name: str, request: Request):
    db = _db()
    tbl = _open_or_404(db, name)
    body = await request.json()
    predicate = body.get("predicate", "").strip()
    if predicate:
        tbl.delete(predicate)
    return {"status": "ok"}


# ── Query (vector search / FTS / scan) ────────────────────────────────────────

@app.post("/v1/table/{name}/query/")
async def query_table(name: str, request: Request):
    db = _db()
    tbl = _open_or_404(db, name)
    body = await request.json()

    limit      = int(body.get("limit", 10))
    offset     = int(body.get("offset", 0))
    vector     = body.get("vector")            # dense query vector
    fts_body   = body.get("full_text_search")  # FTS params
    filter_sql = body.get("filter", "")        # SQL filter predicate
    prefilter  = bool(body.get("prefilter", False))
    metric     = str(body.get("metric", "cosine"))

    try:
        if vector is not None:
            # Dense vector search.
            q = (
                tbl.search(vector, query_type="vector", vector_column_name="embedding")
                   .metric(metric)
                   .limit(limit)
            )
            if filter_sql:
                q = q.where(filter_sql, prefilter=prefilter)
            rows = q.to_list()

        elif fts_body is not None:
            # Full-text search.
            fts_query = fts_body.get("query", "")
            columns   = fts_body.get("columns", ["content"])
            col       = columns[0] if columns else "content"
            # Create FTS index once per (table, column) pair; skip if already indexed.
            fts_key = (name, col)
            if fts_key not in _fts_indexed:
                try:
                    tbl.create_fts_index(col, replace=False)
                    _fts_indexed.add(fts_key)
                except Exception:
                    # Index may already exist or creation failed; attempt search anyway.
                    _fts_indexed.add(fts_key)
            q = tbl.search(fts_query, query_type="fts").limit(limit)
            rows = q.to_list()

        else:
            # Full-table scan with optional SQL filter and offset.
            q = tbl.search().limit(limit + offset)
            if filter_sql:
                q = q.where(filter_sql)
            rows = q.to_list()
            if offset > 0:
                rows = rows[offset:]
            rows = rows[:limit]

    except HTTPException:
        raise
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"Query error: {exc}") from exc

    return {"data": [_row_to_dict(r) for r in rows]}


# ── Arrow helpers ─────────────────────────────────────────────────────────────

def _extract_dims(body: dict) -> int | None:
    """Extract embedding dimensions from the Arrow schema JSON."""
    schema = body.get("schema", {})
    for field in schema.get("fields", []):
        if field.get("name") == "embedding":
            ft = field.get("type", {})
            if ft.get("type") == "fixed_size_list":
                return ft.get("list_size")
    return None


def _to_arrow(schema: pa.Schema, records: list) -> pa.Table:
    """Convert a list of dict records to a PyArrow table matching *schema*."""
    emb_field = schema.field("embedding")
    dims: int | None = None
    if pa.types.is_fixed_size_list(emb_field.type):
        dims = emb_field.type.list_size
    elif records:
        emb0 = records[0].get("embedding")
        if emb0:
            dims = len(emb0)

    ids, contents, metadatas, embeddings = [], [], [], []
    for rec in records:
        ids.append(str(rec.get("id", "")))
        contents.append(str(rec.get("content", "") or ""))
        metadatas.append(str(rec.get("metadata", "") or ""))
        emb = rec.get("embedding")
        if emb is not None:
            embeddings.append([float(x) for x in emb])
        else:
            embeddings.append([0.0] * (dims or 1))

    return pa.table({
        "id":        pa.array(ids,        type=pa.utf8()),
        "content":   pa.array(contents,   type=pa.utf8()),
        "metadata":  pa.array(metadatas,  type=pa.utf8()),
        "embedding": pa.array(embeddings, type=emb_field.type),
    })


def _row_to_dict(row: dict) -> dict:
    """Convert a LanceDB result row to a JSON-serializable dict."""
    out: dict = {
        "id":       str(row.get("id", "")),
        "content":  str(row.get("content",  "") or ""),
        "metadata": str(row.get("metadata", "") or ""),
    }
    dist = row.get("_distance")
    if dist is not None:
        out["_distance"] = float(dist)
    score = row.get("_score")
    if score is not None:
        out["_score"] = float(score)
    emb = row.get("embedding")
    if emb is not None:
        try:
            out["embedding"] = emb.tolist() if hasattr(emb, "tolist") else [float(x) for x in emb]
        except Exception:
            out["embedding"] = []
    return out


# ── Entry point ───────────────────────────────────────────────────────────────

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8181, log_level="info")
