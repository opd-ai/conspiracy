# Dockerfile for integration test runner
FROM golang:1.25-alpine

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build test binary
RUN go test -c -tags=integration -o /integration-test ./test/integration

# Run tests
CMD ["/integration-test", "-test.v"]
