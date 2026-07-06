# LanceDB local REST server for integration testing.
# Based on the LanceDB project's own base image (python:3.12-slim-bookworm + Rust),
# then adds FastAPI/uvicorn to expose a REST server on port 8181.

FROM python:3.12-slim-bookworm

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl build-essential && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY server.py .

EXPOSE 8181

HEALTHCHECK --interval=10s --timeout=5s --retries=10 --start-period=60s \
    CMD curl -sf http://localhost:8181/v1/table/ || exit 1

CMD ["python", "server.py"]
