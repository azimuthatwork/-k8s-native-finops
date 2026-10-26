# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the exporter
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o /exporter ./cmd/exporter

# Final stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 -S exporter && \
    adduser -u 1000 -S exporter -G exporter

WORKDIR /app

# Copy binary from builder
COPY --from=builder /exporter /app/exporter

# Change ownership
RUN chown -R exporter:exporter /app

USER exporter

# Run the exporter
ENTRYPOINT ["/app/exporter"]
