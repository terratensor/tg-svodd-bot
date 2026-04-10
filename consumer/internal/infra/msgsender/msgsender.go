package msgsender

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/buttonscheduler"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/msgparser"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/mtproto"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/metrics"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/repos/tgmessage"
)

var (
	mtprotoClient     *mtproto.Client
	mtprotoClientOnce sync.Once
	mtprotoCtx        context.Context
	mtprotoCancel     context.CancelFunc
	mtprotoReady      bool
)

// initMTProto инициализирует MTProto клиент если прокси включен
func initMTProto() {
	mtprotoClientOnce.Do(func() {
		if os.Getenv("TG_WS_PROXY_ENABLED") != "true" {
			log.Printf("📡 MTProto disabled")
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

		// Запускаем подключение в горутине
		go func() {
			log.Printf("🔌 Connecting MTProto client...")
			if err := mtprotoClient.Connect(mtprotoCtx); err != nil {
				log.Printf("❌ MTProto connect failed: %v", err)
				return
			}
			mtprotoReady = true
			log.Printf("✅ MTProto client ready")
		}()
	})
}

// shutdownMTProto закрывает MTProto клиент
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

	// Инициализируем MTProto если прокси включен
	initMTProto()

	// Ждем готовности MTProto клиента перед отправкой
	if os.Getenv("TG_WS_PROXY_ENABLED") == "true" {
		log.Printf("⏳ Waiting for MTProto client to be ready...")
		for i := 0; i < 60; i++ {
			if mtprotoReady && mtprotoClient != nil && mtprotoClient.IsReady() {
				log.Printf("✅ MTProto client ready, proceeding with send")
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	defer shutdownMTProto()

	chatID := os.Getenv("TG_CHAT_ID")

	// Парсим ID комментария
	commentID, err := strconv.Atoi(headers["comment_id"])
	if err != nil {
		log.Printf("can not parse comment_id %v", err)
		commentID = 0
	}

	// Проверяем готовность MTProto клиента
	useMTProto := os.Getenv("TG_WS_PROXY_ENABLED") == "true" && mtprotoClient != nil && mtprotoClient.IsReady()

	// Если MTProto готов и есть форматированное сообщение - отправляем через него
	if useMTProto && parsedResult.Formatted != nil {
		log.Printf("🚀 Sending formatted message via MTProto")

		var messageID int32
		var sendErr error

		for attempt := 1; attempt <= 100; attempt++ {
			var id int
			var err error

			if buttonScheduler.ShouldShowButton() {
				qurl, _ := cleanQuestionURL(headers["comment_link"])
				id, err = mtprotoClient.SendFormattedMessageWithButton(ctx, chatID,
					parsedResult.Formatted, "Подключайтесь к соборному интеллекту", qurl)
				buttonScheduler.Reset()
			} else {
				id, err = mtprotoClient.SendFormattedMessage(ctx, chatID, parsedResult.Formatted)
			}

			if err != nil {
				sendErr = err
				cm := fmt.Sprintf("MTProto send error (attempt %d/100): %v", attempt, err)
				log.Println(cm)
				sentry.CaptureMessage(cm)
				time.Sleep(time.Second * 5)
				continue
			}

			messageID = int32(id)
			sendErr = nil
			log.Printf("✅ Message sent via MTProto: ID=%d", messageID)
			break
		}

		if sendErr != nil {
			log.Printf("❌ Failed to send via MTProto: %v", sendErr)
			return
		}

		// Сохраняем в БД
		m.MessagesSent.WithLabelValues().Inc()
		tgMessage := tgmessage.TgMessage{
			CommentID: commentID,
			MessageID: messageID,
		}
		if err := tgmessages.Create(context.Background(), tgMessage); err != nil {
			log.Printf("error create tgmessage ID: %v", err)
		} else {
			log.Printf("tgMessage saved: %+v", tgMessage)
		}

		return
	}

	// Fallback: используем HTML сообщения
	log.Printf("📡 Falling back to HTML messages")
	messages := parsedResult.Messages

	for i, text := range messages {
		// Для fallback используем текст как есть
		msgText := text

		shouldShowButton := buttonScheduler.ShouldShowButton() && i == len(messages)-1
		if shouldShowButton {
			buttonScheduler.Reset()
		}

		var messageID int32
		var sendErr error

		for attempt := 1; attempt <= 100; attempt++ {
			if useMTProto {
				// Отправляем простой текст через MTProto
				id, err := mtprotoClient.SendMessage(ctx, chatID, msgText)
				if err != nil {
					sendErr = err
					cm := fmt.Sprintf("MTProto send error (attempt %d/100): %v", attempt, err)
					log.Println(cm)
					sentry.CaptureMessage(cm)
					time.Sleep(time.Second * 5)
					continue
				}
				messageID = int32(id)
				sendErr = nil
				log.Printf("✅ Message sent via MTProto (plain): ID=%d", messageID)
				break
			} else {
				sendErr = fmt.Errorf("MTProto client not available")
				log.Printf("❌ %v", sendErr)
				break
			}
		}

		if sendErr != nil {
			log.Printf("❌ Failed to send message: %v", sendErr)
			continue
		}

		m.MessagesSent.WithLabelValues().Inc()

		tgMessage := tgmessage.TgMessage{
			CommentID: commentID,
			MessageID: messageID,
		}

		if err := tgmessages.Create(context.Background(), tgMessage); err != nil {
			log.Printf("error create tgmessage ID: %v", err)
		} else {
			log.Printf("tgMessage saved: %+v", tgMessage)
		}

		time.Sleep(time.Second * 3)
	}
}

// cleanQuestionURL очищает URL от фрагментов
func cleanQuestionURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("ошибка при разборе URL: %v", err)
	}
	parsedURL.Fragment = ""
	return parsedURL.String(), nil
}
