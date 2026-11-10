FROM golang:1.23-alpine AS build

WORKDIR /src
RUN apk add --no-cache postgresql su-exec
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY migrations ./migrations
COPY scripts ./scripts

RUN COVERAGE_FILE=/tmp/coverage.out sh scripts/test-coverage.sh \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/knowledge-api ./cmd/server

FROM alpine:3.20

RUN adduser -D -u 10001 appuser && apk add --no-cache ca-certificates && mkdir -p /data/jira && chown appuser /data/jira
COPY --from=build /out/knowledge-api /usr/local/bin/knowledge-api

USER appuser
EXPOSE 8000
ENTRYPOINT ["/usr/local/bin/knowledge-api"]

