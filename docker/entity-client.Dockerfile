# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy the root module files first (this is the simulation module)
COPY go.mod go.sum ./
COPY shared/ ./shared/
COPY proto/ ./proto/

# Copy the client module
COPY cmd/entity-client/ ./cmd/entity-client/

# Download dependencies for the root module first
RUN go mod download

# Download dependencies for the client module
WORKDIR /app/cmd/entity-client
RUN go mod download

# Build the application from the client module directory
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o entity-client .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/cmd/entity-client/entity-client .

# Create a non-root user and set up writable directory
RUN adduser -D -s /bin/sh appuser && \
    chown -R appuser:appuser /app

USER appuser

ENTRYPOINT ["/app/entity-client"] 