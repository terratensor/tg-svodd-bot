package msghandler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"tg-svodd-bot/consumer/internal/infra/msgparser"
	"tg-svodd-bot/consumer/internal/infra/msgsender"
	"tg-svodd-bot/consumer/internal/repos/tgmessage"

	"github.com/getsentry/sentry-go"
)

type Request struct {
	ID      string
	Message string
	Headers map[string]string
}

func Handler(ctx context.Context, ch chan Request, wg *sync.WaitGroup, tgmessages *tgmessage.TgMessages) {
	defer wg.Done()

	parser := msgparser.New()

	for {
		select {
		case <-ctx.Done():
			return
		case r := <-ch:

			log.Printf("id=%s metadata=%+v", r.ID, r.Headers)

			// Обрабатываем комментарий, заменяем, удаляем не поддерживаемые теги,
			// форматируем и разбиваю на блоки не превышающие 4096 символов,
			// для отправки в телеграм
			messages, err := parser.Parse(r.Message)
			if err != nil {
				sentry.CaptureMessage(fmt.Sprint(err))
				log.Printf("error: %v Text: %s", err, r.Message)
			}
			// Отправляем подготовленные сообщения в телеграм
			msgsender.Send(ctx, messages, r.Headers, tgmessages)
		}
	}
}
