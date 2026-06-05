FROM golang:alpine AS builder

WORKDIR /app

# Copy source files FIRST so Go can see the imports
COPY . .

# Initialize Go module and resolve dependencies
RUN go mod init github.com/Garletz/gafam/vpc-relay && \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o vpc-relay .

FROM alpine:latest

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/vpc-relay /app/

RUN mkdir -p /app/data

VOLUME ["/app/data"]

# Port 5150: GAFAM VPC HTTPS relay (self-signed TLS)
EXPOSE 5150
EXPOSE 5151

CMD ["/app/vpc-relay"]
