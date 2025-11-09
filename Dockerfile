# syntax=docker/dockerfile:1

FROM golang:1.22 AS build
WORKDIR /app

# Pull Go dependencies first for better layer caching
COPY src/go.mod src/go.sum ./
RUN go mod download

# Copy the rest of the project (keep within the Go module)
COPY src/ ./

# Build the binary statically so it runs in a minimal runtime image
RUN CGO_ENABLED=0 GOOS=linux go build -o telbot

FROM gcr.io/distroless/base-debian12
WORKDIR /app

# Copy the compiled binary and runtime assets
COPY --from=build /app/telbot ./telbot
COPY src/configs ./configs
COPY src/states.json ./states.json
COPY src/.env ./.env

# Run the Telegram bot
ENTRYPOINT ["./telbot"]
