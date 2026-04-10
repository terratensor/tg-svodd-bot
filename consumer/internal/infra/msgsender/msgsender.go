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
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/mtproto"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/metrics"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/repos/tgmessage"
)

var (
	mtprotoClient     *mtproto.Client
	mtprotoClientOnce sync.Once
	mtprotoCtx        context.Context
	mtprotoCancel     context.CancelFunc
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
			log.Printf("✅ MTProto client ready")
		}()

		// Ждем готовности (до 30 секунд)
		for i := 0; i < 60; i++ {
			if mtprotoClient.IsReady() {
				log.Printf("✅ MTProto client is ready to send messages")
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
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
func Send(ctx context.Context, messages []string, headers map[string]string, tgmessages *tgmessage.TgMessages,
	m *metrics.Metrics, buttonScheduler *buttonscheduler.ButtonScheduler) {

	// Инициализируем MTProto если прокси включен
	initMTProto()
	defer shutdownMTProto()

	chatID := os.Getenv("TG_CHAT_ID")

	// Парсим ID комментария
	commentID, err := strconv.Atoi(headers["comment_id"])
	if err != nil {
		log.Printf("can not parse comment_id %v", err)
		commentID = 0
	}

	// Проверяем готовность MTProto клиента
	if os.Getenv("TG_WS_PROXY_ENABLED") == "true" {
		if mtprotoClient == nil || !mtprotoClient.IsReady() {
			log.Printf("❌ MTProto client not ready, message will be lost")
			return
		}
	}

	for i, text := range messages {
		// Подготавливаем текст сообщения
		var msgText string
		if buttonScheduler.ShouldShowButton() && i == len(messages)-1 {
			qurl, err := cleanQuestionURL(headers["comment_link"])
			if err == nil {
				msgText = text + "\n\n🔗 " + qurl
			} else {
				msgText = text
			}
			buttonScheduler.Reset()
		} else {
			msgText = text
		}

		// Пытаемся отправить через MTProto
		var messageID int32
		var sendErr error

		for attempt := 1; attempt <= 100; attempt++ {
			if mtprotoClient != nil && mtprotoClient.IsReady() {
				id, err := mtprotoClient.SendMessage(ctx, chatID, msgText)
				if err != nil {
					sendErr = err
					cm := fmt.Sprintf("MTProto send error (attempt %d/5): %v", attempt, err)
					log.Println(cm)
					sentry.CaptureMessage(cm)
					time.Sleep(time.Second * 5)
					continue
				}
				messageID = int32(id)
				sendErr = nil
				log.Printf("✅ Message sent via MTProto: ID=%d", messageID)
				break
			} else {
				sendErr = fmt.Errorf("MTProto client not available")
				log.Printf("❌ %v", sendErr)
				break
			}
		}

		if sendErr != nil {
			log.Printf("❌ Failed to send message after all attempts: %v", sendErr)
			continue
		}

		// Фиксируем метрику при отправке сообщения
		m.MessagesSent.WithLabelValues().Inc()

		// Формируем сообщение в БД
		tgMessage := tgmessage.TgMessage{
			CommentID: commentID,
			MessageID: messageID,
		}

		// Сохраняем ID сообщения в БД
		err = tgmessages.Create(context.Background(), tgMessage)
		if err != nil {
			log.Printf("error create tgmessage ID: %v", err)
		} else {
			log.Printf("tgMessage saved: %+v", tgMessage)
		}

		// Ожидаем 3 секунды после отправки
		time.Sleep(time.Second * 3)
	}
}

// cleanQuestionURL очищает URL от фрагментов
func cleanQuestionURL(rawURL string) (string, error) {
	// Парсим URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("ошибка при разборе URL: %v", err)
	}

	// Удаляем фрагмент (часть после #)
	parsedURL.Fragment = ""

	// Возвращаем очищенный URL в виде строки
	return parsedURL.String(), nil
}
