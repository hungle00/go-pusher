FROM golang:1.26.2-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY internal/ ./internal/
COPY public/ ./public/

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/go-pusher . \
    && mkdir -p /out/data

FROM scratch

WORKDIR /app

COPY --from=builder --chown=65532:65532 /out/go-pusher /go-pusher
COPY --from=builder --chown=65532:65532 /out/data /data
COPY --from=builder --chown=65532:65532 /src/public ./public

ENV PUSHER_DB_PATH=/data/pusher.db

EXPOSE 8080
VOLUME ["/data"]
USER 65532:65532

ENTRYPOINT ["/go-pusher"]