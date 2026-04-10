package msgsender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/domain/message"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/buttonscheduler"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/msgparser"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/mtproto"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/metrics"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/repos/tgmessage"
)

// HTTPError represents an HTTP error returned by a server.
type HTTPError struct {
	StatusCode int
	Status     string
}

func (err HTTPError) Error() string {
	return fmt.Sprintf("failed to send successful request. Status was %s", err.Status)
}

type Msgresponse struct {
	Ok     bool
	Result map[string]interface{}
}

var (
	mtprotoClient     *mtproto.Client
	mtprotoClientOnce sync.Once
	mtprotoCtx        context.Context
	mtprotoCancel     context.CancelFunc
)

func initMTProto() {
	mtprotoClientOnce.Do(func() {
		if os.Getenv("TG_WS_PROXY_ENABLED") != "true" {
			log.Printf("📡 MTProto disabled, using HTTP API")
			return
		}

		log.Printf("🔄 Initializing MTProto client...")
		mtprotoCtx, mtprotoCancel = context.WithCancel(context.Background())

		client, err := mtproto.New(mtprotoCtx)
		if err != nil {
			log.Printf("❌ Failed to create MTProto client: %v", err)
			return
		}

		mtprotoClient = client

		go func() {
			log.Printf("🔌 Connecting MTProto client...")
			if err := mtprotoClient.Connect(mtprotoCtx); err != nil {
				log.Printf("❌ MTProto connect failed: %v", err)
				return
			}
			log.Printf("✅ MTProto client ready")
		}()

		for i := 0; i < 60; i++ {
			if mtprotoClient.IsReady() {
				log.Printf("✅ MTProto client is ready to send messages")
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	})
}

func shutdownMTProto() {
	if mtprotoCancel != nil {
		mtprotoCancel()
	}
	if mtprotoClient != nil {
		mtprotoClient.Close()
	}
}

// Send отправляет сообщения в Telegram
func Send(ctx context.Context, parsedResult *msgparser.ParsedResult, headers map[string]string,
	tgmessages *tgmessage.TgMessages, m *metrics.Metrics, buttonScheduler *buttonscheduler.ButtonScheduler) {

	initMTProto()
	defer shutdownMTProto()

	chatID := os.Getenv("TG_CHAT_ID")
	commentID, _ := strconv.Atoi(headers["comment_id"])

	// Проверяем, можем ли использовать MTProto
	useMTProto := os.Getenv("TG_WS_PROXY_ENABLED") == "true" && mtprotoClient != nil && mtprotoClient.IsReady()

	// Получаем токен для HTTP API
	contents, _ := os.ReadFile(os.Getenv("TG_BOT_TOKEN_FILE"))
	token := fmt.Sprintf("%v", strings.Trim(string(contents), "\r\n"))

	// Если есть форматированное сообщение и MTProto готов - пробуем отправить через MTProto
	if useMTProto && parsedResult.Formatted != nil {
		log.Printf("🚀 Attempting to send via MTProto")

		var msgID int
		var err error

		if buttonScheduler.ShouldShowButton() {
			qurl, _ := cleanQuestionURL(headers["comment_link"])
			msgID, err = mtprotoClient.SendFormattedMessageWithButton(ctx, chatID, parsedResult.Formatted,
				"Подключайтесь к соборному интеллекту", qurl)
			buttonScheduler.Reset()
		} else {
			msgID, err = mtprotoClient.SendFormattedMessage(ctx, chatID, parsedResult.Formatted)
		}

		if err == nil {
			log.Printf("✅ Message sent via MTProto: ID=%d", msgID)
			saveMessage(ctx, tgmessages, commentID, int32(msgID), m)
			return
		}

		log.Printf("⚠️ MTProto send failed: %v, falling back to HTTP", err)
	}

	// Fallback на HTTP API с оригинальной логикой
	log.Printf("📡 Sending via HTTP API")

	for i, htmlMsg := range parsedResult.Messages {
		msg := &message.Message{
			ChatID:    chatID,
			Text:      htmlMsg,
			ParseMode: "HTML",
		}

		// Проверяем, нужно ли показывать кнопку (только на последнем сообщении)
		if buttonScheduler.ShouldShowButton() && i == len(parsedResult.Messages)-1 {
			qurl, err := cleanQuestionURL(headers["comment_link"])
			if err == nil {
				button := message.InlineButton{
					Text:         "Подключайтесь к соборному интеллекту",
					CallbackData: qurl,
					URL:          qurl,
				}

				inlineKeyboard := make([][]message.InlineButton, 1)
				inlineKeyboard[0] = append(inlineKeyboard[0], button)

				msg.ReplyMarkup = &message.ReplyMarkup{
					InlineKeyboard: inlineKeyboard,
				}
			}
			buttonScheduler.Reset()
		}

		tgUrl := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)

		for attempt := 1; attempt <= 100; attempt++ {
			messageID, err := sendMessageHTTP(tgUrl, msg)
			if err != nil {
				cm := fmt.Sprintf("error: %v Text: %s", err, msg.Text)
				log.Println(cm)
				sentry.CaptureMessage(cm)
				time.Sleep(time.Second * 5)
				continue
			}

			m.MessagesSent.WithLabelValues().Inc()
			saveMessage(ctx, tgmessages, commentID, *messageID, m)
			break
		}

		time.Sleep(time.Second * 3)
	}
}

func sendMessageHTTP(url string, message *message.Message) (*int32, error) {
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}

	client := http.Client{Timeout: 10 * time.Second}
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
		log.Printf("failed to decode response body %v", err)
	} else {
		messageID = int32(j.Result["message_id"].(float64))
	}

	return &messageID, nil
}

func saveMessage(ctx context.Context, tgmessages *tgmessage.TgMessages, commentID int, messageID int32, m *metrics.Metrics) {
	tgMessage := tgmessage.TgMessage{
		CommentID: commentID,
		MessageID: messageID,
	}

	if err := tgmessages.Create(ctx, tgMessage); err != nil {
		log.Printf("error create tgmessage: %v", err)
	} else {
		log.Printf("tgMessage saved: %+v", tgMessage)
	}
}

func cleanQuestionURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("ошибка при разборе URL: %v", err)
	}
	parsedURL.Fragment = ""
	return parsedURL.String(), nil
}
