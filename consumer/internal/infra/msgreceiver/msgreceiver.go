package msgreceiver

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"tg-svodd-bot/consumer/internal/infra/msghandler"

	"github.com/getsentry/sentry-go"

	"gocloud.dev/pubsub"
)

func Run(ctx context.Context, chout chan msghandler.Request, wg *sync.WaitGroup) error {
	defer wg.Done()

	// pubsub.OpenSubscription creates a *pubsub.Subscription from a URL.
	// This URL will Dial the RabbitMQ server at the URL in the environment
	// variable RABBIT_SERVER_URL and open the queue "myqueue".
	subs, err := pubsub.OpenSubscription(ctx, os.Getenv("Q1"))
	if err != nil {
		return err
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
			log.Printf("Receiving message: %v", err)
			return err
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
