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
		Body: []byte("Нет ничего смешнее, чем когда наркоманский бред выдаётся за логику\n\nМОСКВА, 27 августа. /ТАСС/. Президент Украины Владимир Зеленский допустил проведение выборов до окончания боевых действий, если западные страны профинансируют их проведение. По словам Зеленского, если Запад готов предоставить средства, то на проведение выборов в мирное время нужно около $5 млрд, а назвать сумму, нужную в условиях боевых действий назвать затруднился.\n\nА теперь слова Гитлера-Зеленского, начинающиеся со слова логика ))))))\n\n«Логика в том, что если защищаешь демократию, то во время войны ты должен об этой защите думать, а выборы - один элемент защиты. Но согласно законодательству, люди же не просто так это сделали, запрещены выборы. Провести их сложно. <...> Проводить в кредит выборы я не буду, забирать деньги от оружия и давать их на выборы также не буду <...>. Но, самое главное - давайте рисковать тогда вместе. Наблюдатели должны быть в окопах»\n\nЩ — логика (с)\n\nВот в этой «логике» и проживают либердосы. Именно такими дырами сложена нейросеть (если можно её так называть) калейдоскопа среднего либерала."),
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
