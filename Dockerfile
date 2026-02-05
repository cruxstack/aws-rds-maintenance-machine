# UI build stage
FROM node:22-alpine AS ui-builder

WORKDIR /app/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# Go build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install git for fetching dependencies
RUN apk add --no-cache git

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy UI build output to embedded location
COPY --from=ui-builder /app/ui/dist /app/internal/app/ui/dist

# Build arguments for target binary
ARG TARGET=demo

# Build the target binary
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /app/bin/app ./cmd/${TARGET}

# Runtime stage
FROM alpine:3.20

WORKDIR /app

# Install ca-certificates for HTTPS calls
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -g '' appuser

# Create data directory
RUN mkdir -p /app/data && chown appuser:appuser /app/data

# Copy binary from builder (UI is embedded via go:embed)
COPY --from=builder /app/bin/app /app/app

USER appuser

# Default environment variables
ENV APP_DATA_DIR=/app/data
ENV APP_PORT=8080

EXPOSE 8080 9080

ENTRYPOINT ["/app/app"]
