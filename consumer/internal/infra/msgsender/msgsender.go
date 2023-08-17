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

	for _, text := range messages {

		msg := &message.Message{
			ChatID:    chatID,
			Text:      text,
			ParseMode: "HTML",
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
