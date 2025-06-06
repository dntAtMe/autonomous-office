# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the root module files first (this is the simulation module)
COPY go.mod go.sum ./
COPY shared/ ./shared/
COPY proto/ ./proto/

# Copy the server module
COPY cmd/simulation-server/ ./cmd/simulation-server/

# Download dependencies for the root module first
RUN go mod download

# Download dependencies for the server module
WORKDIR /app/cmd/simulation-server
RUN go mod download

# Build the application from the server module directory
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o simulation-server .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/cmd/simulation-server/simulation-server .

# Create a non-root user and set up writable directory
RUN adduser -D -s /bin/sh appuser && \
    chown -R appuser:appuser /app

USER appuser

# Expose HTTP health and gRPC ports
EXPOSE 8080 9090

CMD ["./simulation-server"] 