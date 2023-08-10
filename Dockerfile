# syntax = docker/dockerfile:1.2
FROM golang:latest AS builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

#RUN --mount=type=secret,id=_env,dst=/etc/secrets/.env cat /etc/secrets/.env
#RUN --mount=type=secret,id=tg_bot_token,dst=/run/secrets/tg_bot_token

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o ./tg-svodd-bot ./consumer

# 2

FROM scratch

WORKDIR /app

COPY --from=builder /app/tg-svodd-bot /app/tg-svodd-bot
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
ENV TZ=Europe/Moscow

#ENV RABBIT_SERVER_URL=amqp://rmq
#ENV Q1=rabbit://q1
#ENV TG_CHAT_ID=@svoddru

#ENV TG_BOT_TOKEN=6021633285:AAEJ6UzByuG3Z1Xj9wDzwXyBxbhxoddL_jk

CMD ["./tg-svodd-bot"]