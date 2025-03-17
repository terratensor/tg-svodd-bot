package msgsender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"tg-svodd-bot/consumer/internal/domain/message"
	"tg-svodd-bot/consumer/internal/metrics"
	"tg-svodd-bot/consumer/internal/repos/tgmessage"
	"time"

	"github.com/getsentry/sentry-go"
)

// HTTPError represents an HTTP error returned by a server.
type HTTPError struct {
	StatusCode int
	Status     string
}

// Error returns a string representation of the HTTP error.
//
// No parameters.
// Returns a string.
func (err HTTPError) Error() string {
	return fmt.Sprintf("failed to send successful request. Status was %s", err.Status)
}

type Msgresponse struct {
	Ok     bool
	Result map[string]interface{}
}

func Send(ctx context.Context, messages []string, headers map[string]string, tgmessages *tgmessage.TgMessages, m *metrics.Metrics) {

	contents, _ := os.ReadFile(os.Getenv("TG_BOT_TOKEN_FILE"))
	token := fmt.Sprintf("%v", strings.Trim(string(contents), "\r\n"))

	tgUrl := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage",
		token)
	chatID := os.Getenv("TG_CHAT_ID")

	// Парсим ID комментария
	commentID, err := strconv.Atoi(headers["comment_id"])
	if err != nil {
		log.Printf("can not parse comment_id %v", err)
		commentID = 0
	}

	for _, text := range messages {

		msg := &message.Message{
			ChatID:    chatID,
			Text:      text,
			ParseMode: "HTML",
		}

		for i := 1; i <= 100; i++ {
			// Делаем 100 попыток отправки, если получена ошибка, если нет, то цикл сразу завершается
			messageID, err := sendMessage(tgUrl, msg)
			if err != nil {
				cm := fmt.Sprintf("error: %v Text: %s", err, msg.Text)
				log.Println(cm)
				sentry.CaptureMessage(cm)
				time.Sleep(time.Second * 5)
				continue
			}

			// Фиксируем метрику при отправке сообщения
			m.MessagesSent.WithLabelValues().Inc()

			// Формируем сообщение в БД
			tgMessage := tgmessage.TgMessage{
				CommentID: commentID,
				MessageID: *messageID,
			}

			// Сохраняем ID сообщения в БД
			err = tgmessages.Create(context.Background(), tgMessage)
			if err != nil {
				log.Printf("error create tgmessage ID: %v", err)
			} else {
				log.Printf("tgMessage: %+v", tgMessage)
			}

			break
		}

		// Ожидаем 3 секунды после отправки, необходимо для соблюдения лимитов отправки сообщений ботом, 20 сообщений в минуту
		time.Sleep(time.Second * 3)
	}
}

// sendMessage sends a message to given URL.
func sendMessage(url string, message *message.Message) (*int32, error) {
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}

	// Отправляем запрос с установленным ограниченеием на ответ в 10 секунд.
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	response, err := client.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &HTTPError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
		}
	}

	var j Msgresponse
	var messageID int32

	err = json.NewDecoder(response.Body).Decode(&j)
	if err != nil {
		log.Printf("filed to decode response body %v", err)
	} else {
		log.Printf("response body: %v", j)
		messageID = int32(j.Result["message_id"].(float64))
	}

	return &messageID, nil
}
