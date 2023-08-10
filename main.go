package main

import (
	"context"
	"fmt"
	"log"

	"gocloud.dev/pubsub"
	_ "gocloud.dev/pubsub/rabbitpubsub"
)

func main() {
	ctx := context.Background()

	// pubsub.OpenTopic creates a *pubsub.Topic from a URL.
	// This URL will Dial the RabbitMQ server at the URL in the environment
	// variable RABBIT_SERVER_URL and open the exchange "myexchange".
	topic, err := pubsub.OpenTopic(ctx, "rabbit://ex1")
	if err != nil {
		log.Fatal(err)
	}
	defer func(topic *pubsub.Topic, ctx context.Context) {
		err = topic.Shutdown(ctx)
		if err != nil {
			log.Panic(err)
		}
	}(topic, ctx)

	err = topic.Send(ctx, &pubsub.Message{
		Body: []byte("Hello, <blockquote>World!</blockquote>"),
		// Metadata is optional and can be nil.
		Metadata: map[string]string{
			// These are examples of metadata.
			// There is nothing special about the key names.
			"language":   "en",
			"importance": "high",
		},
	})
	if err != nil {
		log.Println(fmt.Errorf("%v", err))
	}
}
