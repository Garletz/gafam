FROM golang:1.22-alpine AS builder

WORKDIR /app

# We initialize the Go module inside Docker to avoid needing Go locally
RUN go mod init github.com/Garletz/gafam/vpc-relay
RUN go get modernc.org/sqlite
RUN go get github.com/golang-jwt/jwt/v5
RUN go mod tidy

COPY . .

# Build a statically linked binary (CGO_ENABLED=0 is critical for running on scratch/alpine)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o vpc-relay .

FROM alpine:latest

WORKDIR /app

# Install root certificates in case we need to make HTTPS requests later
RUN apk add --no-cache ca-certificates

# Copy the binary from the builder
COPY --from=builder /app/vpc-relay /app/

# Create the data directory for SQLite
RUN mkdir -p /app/data

VOLUME ["/app/data"]

EXPOSE 5150

ENTRYPOINT ["/app/vpc-relay"]
