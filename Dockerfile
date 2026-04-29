# Build stage
FROM golang:1.22-alpine AS builder

# Set working directory
WORKDIR /app

# Copy source code
COPY . .

# Build the application
# We disable CGO to ensure the binary is static and works in the alpine image
RUN CGO_ENABLED=0 GOOS=linux go build -o raftkv ./cmd/raftkv

# Final stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates in case the app needs to make HTTPS calls
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /app/raftkv .

# Create a default data directory
RUN mkdir -p /app/data

ENV PORT=3001

ENTRYPOINT ["./raftkv"]
