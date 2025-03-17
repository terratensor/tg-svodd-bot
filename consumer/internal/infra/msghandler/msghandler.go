package msghandler

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/getsentry/sentry-go"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/buttonscheduler"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/msgparser"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/msgsender"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/metrics"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/repos/tgmessage"
)

type Request struct {
	ID      string
	Message string
	Headers map[string]string
}

func Handler(
	ctx context.Context, ch chan Request, wg *sync.WaitGroup, tgmessages *tgmessage.TgMessages,
	m *metrics.Metrics, buttonScheduler *buttonscheduler.ButtonScheduler) {

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

				// Удаляем подпись, которая начинается с \n\n, содержит ★, ссылку и слово "Источник"
				reSignature := regexp.MustCompile(`★ <a href="https://xn----8sba0bbi0cdm\.xn--p1ai/qa/question[^>]*">Источник</a>`)
				cleanedMessage = reSignature.ReplaceAllString(cleanedMessage, "")

				// Удаляем переносы строк
				cleanedMessage = strings.Trim(cleanedMessage, "\n")

				// Подсчитываем длину сообщения в рунах
				messageRunes := []rune(cleanedMessage)
				if len(messageRunes) <= 216 {
					// Помечаем сообщение как заблокированное
					m.MessagesBlocked.WithLabelValues().Inc()
					log.Printf("Message blocked due to insufficient length: %s", message)
					continue
				}
			}

			// Отправляем подготовленные сообщения в телеграм
			msgsender.Send(ctx, messages, r.Headers, tgmessages, m, buttonScheduler)
		}
	}
}
