package msgsender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"tg-svodd-bot/consumer/internal/domain/message"
	"time"

	"github.com/getsentry/sentry-go"
)

func Send(ctx context.Context, messages []string) {

	contents, _ := os.ReadFile(os.Getenv("TG_BOT_TOKEN_FILE"))
	token := fmt.Sprintf("%v", strings.Trim(string(contents), "\r\n"))

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage",
		token)
	chatID := os.Getenv("TG_CHAT_ID")

	button := message.InlineButton{
		Text: "Открыть комментарий на ФКТ",
		URL:  "https://xn----8sba0bbi0cdm.xn--p1ai/qa/question/view-5723#:~:text=13:07%2017.08.2023",
	}

	link := "\n\n<a href=\"https://фкт-алтай.рф/qa/question/view-5290#:~:text=16:38%2027.08.2023\">Перейти к комментарию на ФКТ</a>"

	inlineKeyboard := make([][]message.InlineButton, 1)
	inlineKeyboard[0] = append(inlineKeyboard[0], button)
	ml := len(messages)

	for n, text := range messages {

		if ml == n+1 {
			text = text + link
		}
		msg := &message.Message{
			ChatID:    chatID,
			Text:      text,
			ParseMode: "HTML",
			ReplyMarkup: message.ReplyMarkup{
				InlineKeyboard: inlineKeyboard,
			},
		}

		err := sendMessage(url, msg)
		if err != nil {
			cm := fmt.Sprintf("error: %v Text: %s", err, msg.Text)
			log.Println(cm)
			sentry.CaptureMessage(cm)
		}
		// Ожидаем 3 секунды после отправки, необходимо для соблюдения лимитов отправки сообщений ботом, 20 сообщений в минуту
		time.Sleep(time.Second * 3)
	}
}

// sendMessage sends a message to given URL.
func sendMessage(url string, message *message.Message) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	response, err := http.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	defer response.Body.Close()

	var j interface{}

	err = json.NewDecoder(response.Body).Decode(&j)
	if err != nil {
		log.Printf("filed to decode response body %v", err)
	} else {
		log.Printf("response body: %v", j)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send successful request. Status was %q. Response body: %v", response.Status, j)
	}
	return nil
}
