package msghandler

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sync"
	"tg-svodd-bot/consumer/internal/infra/msgparser"
	"tg-svodd-bot/consumer/internal/infra/msgsender"
	"tg-svodd-bot/consumer/internal/metrics"
	"tg-svodd-bot/consumer/internal/repos/tgmessage"

	"github.com/getsentry/sentry-go"
)

type Request struct {
	ID      string
	Message string
	Headers map[string]string
}

func Handler(ctx context.Context, ch chan Request, wg *sync.WaitGroup, tgmessages *tgmessage.TgMessages, m *metrics.Metrics) {
	defer wg.Done()

	parser := msgparser.New(tgmessages)

	for {
		select {
		case <-ctx.Done():
			return
		case r := <-ch:

			log.Printf("id=%s metadata=%+v", r.ID, r.Headers)

			// Обрабатываем комментарий, заменяем, удаляем не поддерживаемые теги,
			// форматируем и разбиваю на блоки не превышающие 4096 символов,
			// для отправки в телеграм
			messages, err := parser.Parse(ctx, r.Message, r.Headers)
			if err != nil {
				sentry.CaptureMessage(fmt.Sprint(err))
				log.Printf("error: %v Text: %s", err, r.Message)
			}

			// Если сообщение одно, проверяем его длину без учета цитирования
			if len(messages) == 1 {
				message := messages[0]
				// Удаляем цитирование, обрамленное тегами <i></i>
				re := regexp.MustCompile(`<i>.*?</i>`)
				cleanedMessage := re.ReplaceAllString(message, "")

				// Проверяем длину сообщения
				if len(cleanedMessage) < 216 {
					// Помечаем сообщение как заблокированное
					m.MessagesBlocked.WithLabelValues().Inc()
					log.Printf("Message blocked due to insufficient length: %s", message)
					continue
				}
			}

			// Отправляем подготовленные сообщения в телеграм
			msgsender.Send(ctx, messages, r.Headers, tgmessages, m)
		}
	}
}
