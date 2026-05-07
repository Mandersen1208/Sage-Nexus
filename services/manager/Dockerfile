FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/sage-agent    ./cmd/agent
RUN go build -o /out/sage-manager  ./cmd/manager

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /out/sage-agent   ./sage-agent
COPY --from=builder /out/sage-manager ./sage-manager

# Bundle the default config and prompts.
# These can be overridden at runtime by mounting a volume over /app/config
# or by setting AGENTS_CONFIG to a different path.
COPY config/  ./config/
COPY prompts/ ./prompts/

# /data holds the persistent agent identity key (survives restarts).
# /sage-state holds Sage-owned Copilot auth/cache and workspace state.
VOLUME ["/data", "/sage-state"]

# Default to the manager/orchestrator; override CMD to run sage-agent instead.
CMD ["/app/sage-manager"]
