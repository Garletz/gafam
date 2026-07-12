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
    python3 \
    python3-pip \
    python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && pip3 install --break-system-packages onnxruntime-genai huggingface_hub \
    && mkdir -p /app/data/suparna/model /app/data/suparna/readings

COPY --from=builder /app/vpc-relay /app/vpc-relay
COPY suparna/runner.py /app/suparna/runner.py

VOLUME ["/app/data"]

EXPOSE 5150
EXPOSE 5151

CMD ["/app/vpc-relay"]
