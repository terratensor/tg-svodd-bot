# syntax = docker/dockerfile:1.2
FROM golang:latest AS builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o ./tg-svodd-bot ./consumer

# 2

FROM scratch

WORKDIR /app

COPY --from=builder /app/tg-svodd-bot /app/tg-svodd-bot
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
ENV TZ=Europe/Moscow

EXPOSE 8081

CMD ["./tg-svodd-bot"]