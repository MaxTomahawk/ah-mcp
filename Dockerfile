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

RUN apk add --no-cache ca-certificates su-exec \
    && addgroup -S -g 10001 ahmcp \
    && adduser -S -D -H -u 10001 -G ahmcp ahmcp \
    && mkdir -p /data \
    && chown -R ahmcp:ahmcp /data

COPY --from=build /out/ah-mcp /usr/local/bin/ah-mcp
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 0755 /usr/local/bin/ah-mcp /usr/local/bin/docker-entrypoint.sh

VOLUME ["/data"]
EXPOSE 3000 9876

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh", "/usr/local/bin/ah-mcp"]
CMD ["--transport", "streamable-http", "--remote"]
