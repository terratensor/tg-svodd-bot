package msgreceiver

import (
	"context"
	"gocloud.dev/pubsub"
	"log"
	"os"
	"sync"
	"tg-svodd-bot/consumer/internal/infra/msghandler"
)

func Run(ctx context.Context, chout chan msghandler.Request, wg *sync.WaitGroup) {
	defer wg.Done()

	// pubsub.OpenSubscription creates a *pubsub.Subscription from a URL.
	// This URL will Dial the RabbitMQ server at the URL in the environment
	// variable RABBIT_SERVER_URL and open the queue "myqueue".
	subs, err := pubsub.OpenSubscription(ctx, os.Getenv("Q1"))
	if err != nil {
		log.Fatal(err)
	}
	defer subs.Shutdown(ctx)

	for {
		msg, err := subs.Receive(ctx)
		if err != nil {
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
