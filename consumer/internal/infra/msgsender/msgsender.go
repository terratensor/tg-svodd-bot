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

func Send(ctx context.Context, text string) {

	contents, _ := os.ReadFile(os.Getenv("TG_BOT_TOKEN_FILE"))
	token := fmt.Sprintf("%v", strings.Trim(string(contents), "\r\n"))

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage",
		token)
	chatID := os.Getenv("TG_CHAT_ID")

	msg := &message.Message{
		ChatID:    chatID,
		Text:      text,
		ParseMode: "HTML",
	}

	if len(msg.Text) >= 4096 {
		splitMsg := splitMessage(msg, 4096)
		for _, m := range splitMsg {
			err := sendMessage(url, &message.Message{
				ChatID:    chatID,
				Text:      m,
				ParseMode: "HTML",
			})
			if err != nil {
				cm := fmt.Sprintf("error: %v Text: %s", err, text)
				log.Println(cm)
				sentry.CaptureMessage(cm)
			}
			// Ожидаем 3 секунды после отправки, необходимо для соблюдения лимитов отправки сообщений ботом, 20 сообщений в минуту
			time.Sleep(time.Second * 3)
		}
	} else {
		err := sendMessage(url, msg)
		if err != nil {
			cm := fmt.Sprintf("error: %v Text: %s", err, text)
			log.Println(cm)
			sentry.CaptureMessage(cm)
		}
		// Ожидаем 3 секунды после отправки, необходимо для соблюдения лимитов отправки сообщений ботом, 20 сообщений в минуту
		time.Sleep(time.Second * 3)
	}

}

func splitMessage(msg *message.Message, chunkSize int) []string {
	s := msg.Text

	if len(s) == 0 {
		return nil
	}
	if chunkSize >= len(s) {
		return []string{s}
	}
	var chunks []string = make([]string, 0, (len(s)-1)/chunkSize+1)
	currentLen := 0
	currentStart := 0
	for i := range s {
		if currentLen == chunkSize {
			chunks = append(chunks, s[currentStart:i])
			currentLen = 0
			currentStart = i
		}
		currentLen++
	}
	chunks = append(chunks, s[currentStart:])
	return chunks
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
