FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY cmd/ ./cmd/

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /acp-server . && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /keygen ./cmd/keygen

# ---- runtime ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /acp-server /acp-server
COPY --from=builder /keygen /keygen
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
