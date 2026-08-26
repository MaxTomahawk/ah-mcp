FROM golang:1.23-alpine AS build

ARG VERSION=dev

RUN apk add --no-cache ca-certificates git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/ah-mcp .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates curl su-exec \
    && addgroup -S -g 10001 ahmcp \
    && adduser -S -D -H -u 10001 -G ahmcp ahmcp \
    && mkdir -p /data \
    && chown -R ahmcp:ahmcp /data

COPY --from=build /out/ah-mcp /usr/local/bin/ah-mcp
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 0755 /usr/local/bin/ah-mcp /usr/local/bin/docker-entrypoint.sh

VOLUME ["/data"]
EXPOSE 3000 9876

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=12 \
    CMD curl -sS -D - --max-time 2 http://127.0.0.1:3000/mcp -o /dev/null 2>/dev/null | grep -q '^HTTP/1.1 200'

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh", "/usr/local/bin/ah-mcp"]
CMD ["--transport", "streamable-http", "--remote"]
