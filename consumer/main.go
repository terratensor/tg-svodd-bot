package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"tg-svodd-bot/consumer/internal/infra/msghandler"
	"tg-svodd-bot/consumer/internal/infra/msgreceiver"
	"time"

	"github.com/getsentry/sentry-go"

	_ "gocloud.dev/pubsub/rabbitpubsub"
)

func main() {

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

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

	ch := make(chan msghandler.Request, 100)
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go msgreceiver.Run(ctx, ch, wg)
	go msghandler.Handler(ctx, ch, wg)

	// Flush buffered events before the program terminates.
	defer sentry.Flush(2 * time.Second)

	wg.Wait()
	stop()
}
