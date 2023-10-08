package msgreceiver

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"tg-svodd-bot/consumer/internal/infra/msghandler"

	"github.com/getsentry/sentry-go"

	"gocloud.dev/pubsub"
)

func Run(ctx context.Context, chout chan msghandler.Request, wg *sync.WaitGroup) {
	defer wg.Done()

	// pubsub.OpenSubscription creates a *pubsub.Subscription from a URL.
	// This URL will Dial the RabbitMQ server at the URL in the environment
	// variable RABBIT_SERVER_URL and open the queue "myqueue".
	subs, err := pubsub.OpenSubscription(ctx, os.Getenv("Q1"))
	if err != nil {
		sentry.CaptureMessage(fmt.Sprint(err))
		log.Panic(err)
	}

	defer func(subs *pubsub.Subscription, ctx context.Context) {
		err := subs.Shutdown(ctx)
		if err != nil {
			sentry.CaptureMessage(fmt.Sprint(err))
			log.Panic(err)
		}
	}(subs, ctx)

	for {
		msg, err := subs.Receive(ctx)
		if err != nil {

			sentry.CaptureMessage(fmt.Sprint(err))
			// Осуществляется проверка по содержимому строки ошибки на совпадение текста.
			// Не нашел способа лучше, без привязки к содержимому строки ошибки.
			// На самом деле такая ошибка не должна возникать, т.к когда запущен сервер очередей, уже должна быть создана вся структура и очереди
			// Надо добавить создание очереди q1 на сервисах svodd
			if strings.Contains(fmt.Sprint(err), "NOT_FOUND - no queue 'q1' in vhost '/'") {
				log.Printf("failure, restart required: %v", err)
				os.Exit(1)
			}
			log.Printf("Receiving message: %v", err)
			break
		}
		select {
		case <-ctx.Done():
			break
		default:
		}

		chout <- msghandler.Request{
			ID:      msg.LoggableID,
			Message: string(msg.Body),
			Headers: msg.Metadata,
		}

		msg.Ack()
	}
}
