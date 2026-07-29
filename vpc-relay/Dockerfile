FROM golang:alpine AS builder

WORKDIR /app

ARG VERSION=dev
ARG GIT_SHA=unknown
ARG BUILD_TIME=unknown

COPY . .

RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-w -s -X main.Version=${VERSION} -X main.GitSHA=${GIT_SHA} -X main.BuildTime=${BUILD_TIME}" \
      -o vpc-relay .

FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/vpc-relay /app/vpc-relay
COPY geo-data /app/geo-data

VOLUME ["/app/data"]

EXPOSE 5150
EXPOSE 5151

CMD ["/app/vpc-relay"]
