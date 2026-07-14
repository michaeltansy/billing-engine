# Use the official Golang image as a base
FROM golang:1.26 AS builder

COPY sql/init.sql /docker-entrypoint-initdb.d/

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum to install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire application source
COPY . .

# Build the binary for Linux
RUN go build -o app ./cmd/main.go

# Create a minimal runtime image
FROM debian:bookworm-slim

# Set the working directory in the runtime container
WORKDIR /app

# Copy the compiled binary from the builder
COPY --from=builder /app/app .
COPY --from=builder /app/files ./files

# Expose the application port
EXPOSE 8081

# Run the application
CMD ["./app"]
