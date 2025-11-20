# syntax=docker/dockerfile:1

FROM golang:1.22 AS go-build
WORKDIR /app

# Pull Go dependencies first for better layer caching
COPY src/go.mod src/go.sum ./
RUN go mod download

# Copy the rest of the project (keep within the Go module)
COPY src/ ./

# Build the binary statically so it runs in a minimal runtime image
RUN CGO_ENABLED=0 GOOS=linux go build -o telbot

# --- Telbot runtime image -----------------------------------------------------
FROM gcr.io/distroless/base-debian12 AS telbot
WORKDIR /app

ENV ASSETS_DIR=/app/assets \
    CONFIG_DIR=/app/configs

COPY --from=go-build /app/telbot ./telbot
COPY src/configs ./configs
COPY src/assets ./assets
COPY src/.env ./.env

ENTRYPOINT ["/app/telbot"]

# --- FastAPI dashboard image --------------------------------------------------
FROM python:3.12-slim AS dashboard
WORKDIR /app

ENV PYTHONUNBUFFERED=1 \
    CONFIG_DIR=/app/configs \
    ASSETS_DIR=/app/assets \
    FASTAPI_PORT=8000

COPY pyservice/requirements.txt ./requirements.txt
RUN python -m pip install --no-cache-dir -r requirements.txt

COPY pyservice ./pyservice
COPY src/assets ./assets
COPY src/configs ./configs

EXPOSE 8000

CMD ["sh", "-c", "uvicorn pyservice.app.main:app --host 0.0.0.0 --port ${FASTAPI_PORT:-8000}"]

# --- Model service image --------------------------------------------------
FROM python:3.11-slim AS modelservice
WORKDIR /app

ENV PYTHONUNBUFFERED=1 \
    ASSETS_DIR=/app/assets \
    TRAIN_DATA_DIR=/app/data/train \
    VALIDATION_DATA_DIR=/app/data/validation \
    MODEL_DIR=/app/model \
    WEIGHTS_PATH=/app/model/weights/mobilenet_transfer.keras \
    FASTAPI_PORT=8080

RUN apt-get update \
    && apt-get install -y --no-install-recommends libgl1 libglib2.0-0 \
    && rm -rf /var/lib/apt/lists/*

COPY modelservice/requirements.txt ./requirements.txt
RUN python -m pip install --no-cache-dir -r requirements.txt

COPY modelservice ./modelservice
COPY src/assets ./assets

EXPOSE 8080

CMD ["sh", "-c", "uvicorn modelservice.app.main:app --host 0.0.0.0 --port ${FASTAPI_PORT:-8080}"]
