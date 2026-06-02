FROM rust:1.80-slim as builder

WORKDIR /usr/src/app

# Install required system dependencies
RUN apt-get update && apt-get install -y pkg-config libssl-dev build-essential && rm -rf /var/lib/apt/lists/*

# Copy the source code
COPY . .

# Build the release binary
RUN cargo build --release

# Production image
FROM debian:bookworm-slim
WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y ca-certificates libssl3 && rm -rf /var/lib/apt/lists/*

# Copy the built binary
COPY --from=builder /usr/src/app/target/release/gafam_backend-cli /app/

# Copy config folder for Loco (required for runtime config)
COPY --from=builder /usr/src/app/config /app/config

# Create data directory for SQLite
RUN mkdir -p /app/data

# Provide a volume for the sqlite database
VOLUME ["/app/data"]

# Environment variables
ENV LOCO_ENV=production

EXPOSE 5150

# Run the server
ENTRYPOINT ["/app/gafam_backend-cli", "start"]
