package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"tg-svodd-bot/consumer/internal/db/pgstore"
	"tg-svodd-bot/consumer/internal/infra/msghandler"
	"tg-svodd-bot/consumer/internal/infra/msgreceiver"
	"tg-svodd-bot/consumer/internal/lib/secret"
	"tg-svodd-bot/consumer/internal/repos/tgmessage"
	"time"

	"github.com/getsentry/sentry-go"

	_ "gocloud.dev/pubsub/rabbitpubsub"
)

func main() {

	initializeTimezone()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	mode := os.Getenv("APP_ENV")

	if mode == "prod" {
		contents, err := os.ReadFile(os.Getenv("SENTRY_DSN_FILE"))
		if err != nil {
			log.Fatalf("can not read SENTRY_DSN_FILE")
		}
		dsn := fmt.Sprintf("%v", strings.Trim(string(contents), "\r\n"))

		err = sentry.Init(sentry.ClientOptions{
			Dsn: dsn,
			// Set TracesSampleRate to 1.0 to capture 100%
			// of transactions for performance monitoring.
			// We recommend adjusting this value in production,
			TracesSampleRate: 1.0,
		})
		if err != nil {
			log.Fatalf("sentry.Init: %s", err)
		}
	}

	// Подготавливаем подключение к БД
	dsn := newDBConnectionString()
	pgst, err := pgstore.NewMessages(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pgst.Close()

	tgmessages := tgmessage.NewTgMessages(pgst)

	// Подготавливаем канал для обработки комментариев
	ch := make(chan msghandler.Request, 100)
	wg := &sync.WaitGroup{}
	wg.Add(2)

	go func() {
		err := msgreceiver.Run(ctx, ch, wg)
		// Обрабатываем ошибку и выходим с кодом 1, для того чтобы инициировать перезапуск докер контейнера.
		// Возможно тут имеет смысл сделать сервис проверки health, но пока так
		if err != nil {
			log.Printf("%v\r\n failure, restart required", err)
			sentry.CaptureMessage(fmt.Sprint(err))
			os.Exit(1)
		}
	}()

	go msghandler.Handler(ctx, ch, wg, tgmessages)

	if mode == "PROD" {
		// Flush buffered events before the program terminates.
		defer sentry.Flush(2 * time.Second)
	}

	wg.Wait()
	stop()
}

func newDBConnectionString() string {
	dbUser := os.Getenv("POSTGRES_USER")
	dbPassword := strings.TrimRight(string(secret.Read(os.Getenv("POSTGRES_PASSWORD_FILE"))), "\r\n")
	dbName := os.Getenv("POSTGRES_DB")
	dbHost := os.Getenv("POSTGRES_HOST")

	return fmt.Sprintf("postgresql://%s:%s@%s/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbName)
}

func initializeTimezone() {
	if timezone := os.Getenv("TZ"); timezone != "" {
		if location, err := time.LoadLocation(timezone); err != nil {
			log.Printf("error loading timezone '%s': %v\n", timezone, err)
		} else {
			time.Local = location
		}
	}

	now := time.Now()
	log.Printf("Local timezone: %s. Service started at %s", time.Local.String(),
		now.Format("2006-01-02T15:04:05.000 MST"))
}
