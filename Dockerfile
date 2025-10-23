# Stage 1: Build the UI
FROM node:20-alpine AS ui-builder

WORKDIR /build/ui

# Copy package files
COPY ui/package.json ui/package-lock.json* ./

# Install dependencies
RUN npm ci --quiet

# Copy UI source files
COPY ui/ ./

# Build the UI
RUN npm run build

# Stage 2: Build the Go application
FROM golang:1.23-alpine AS go-builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Copy built UI from previous stage
COPY --from=ui-builder /build/ui/../api/ui/dist ./api/ui/dist

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o /build/bin/go-agent \
    .

# Stage 3: Final runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata curl

# Create non-root user
RUN addgroup -g 1000 goagent && \
    adduser -D -u 1000 -G goagent goagent

WORKDIR /app

# Copy binary from builder
COPY --from=go-builder /build/bin/go-agent /app/go-agent

# Copy example env file for reference
COPY .env.example /app/.env.example

# Create directories for data
RUN mkdir -p /app/documents && \
    chown -R goagent:goagent /app

# Switch to non-root user
USER goagent

# Expose the HTTP API port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1

# Default command
ENTRYPOINT ["/app/go-agent"]
CMD ["serve"]
