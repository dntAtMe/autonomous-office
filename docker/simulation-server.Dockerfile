# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the shared module first
COPY go.mod ./
COPY shared/ ./shared/

# Copy the server module
COPY cmd/simulation-server/go.mod cmd/simulation-server/go.sum ./cmd/simulation-server/
COPY cmd/simulation-server/main.go ./cmd/simulation-server/

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

EXPOSE 8080

CMD ["./simulation-server"] 