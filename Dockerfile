# Stage 1: Build the Go application
FROM golang:1.24 AS builder

# Set the working directory
WORKDIR /app

# Copy Go module files first to leverage caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod tidy

# Copy the rest of the application
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o pl-vatbank-checker

# Stage 2: Create a minimal runtime container
FROM alpine:latest

# Install 7-Zip in the final runtime container
RUN apk add --no-cache p7zip

# Set working directory
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/pl-vatbank-checker .

# Ensure the binary is executable
RUN chmod +x /app/pl-vatbank-checker 

# Expose the application port
EXPOSE 8080

# Run the application
CMD ["/app/pl-vatbank-checker"]
