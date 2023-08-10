package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"tg-svodd-bot/consumer/internal/infra/msghandler"
	"tg-svodd-bot/consumer/internal/infra/msgreceiver"

	_ "gocloud.dev/pubsub/rabbitpubsub"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	ch := make(chan msghandler.Request, 100)
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go msgreceiver.Run(ctx, ch, wg)
	go msghandler.Handler(ctx, ch, wg)
	wg.Wait()
	stop()
}
