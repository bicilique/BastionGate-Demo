FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY web ./web

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/acme-people \
    ./cmd/acme-people

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S acme \
    && adduser -S -G acme acme

COPY --from=build /out/acme-people /usr/local/bin/acme-people

USER acme
EXPOSE 3000

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:3000/ >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/acme-people"]

