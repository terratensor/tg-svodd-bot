package msgsender

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
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
	mtprotoClient *mtproto.Client
	mtprotoMu     sync.RWMutex
	mtprotoCtx    context.Context
	mtprotoCancel context.CancelFunc
	mtprotoOnce   sync.Once
)

// initMTProto запускает менеджер подключений (однократно)
func initMTProto() {
	if os.Getenv("TG_WS_PROXY_ENABLED") != "true" {
		return
	}
	mtprotoOnce.Do(func() {
		mtprotoCtx, mtprotoCancel = context.WithCancel(context.Background())
		go mtprotoConnectionManager(mtprotoCtx)
	})
}

// mtprotoConnectionManager управляет подключением и переподключает при падении
func mtprotoConnectionManager(ctx context.Context) {
	retryDelay := 5 * time.Second
	maxRetryDelay := 5 * time.Minute

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Printf("🔄 Creating MTProto client...")
		client, err := mtproto.New(ctx)
		if err != nil {
			log.Printf("❌ Failed to create MTProto client: %v, retrying in %v", err, retryDelay)
			time.Sleep(retryDelay)
			retryDelay = min(retryDelay*2, maxRetryDelay)
			continue
		}

		log.Printf("🔌 Connecting MTProto client...")
		if err := client.Connect(ctx); err != nil {
			log.Printf("❌ MTProto connect failed: %v, retrying in %v", err, retryDelay)
			client.Close()
			time.Sleep(retryDelay)
			retryDelay = min(retryDelay*2, maxRetryDelay)
			continue
		}

		// Успешно подключились
		mtprotoMu.Lock()
		mtprotoClient = client
		mtprotoMu.Unlock()
		log.Printf("✅ MTProto client connected and ready")
		retryDelay = 5 * time.Second

		// Мониторим здоровье клиента
		for {
			time.Sleep(5 * time.Second)
			if !client.IsReady() {
				log.Printf("⚠️ MTProto client died, reconnecting...")
				mtprotoMu.Lock()
				if mtprotoClient == client {
					mtprotoClient = nil
				}
				mtprotoMu.Unlock()
				client.Close()
				break
			}
		}
	}
}

// getClient возвращает текущего живого клиента
func getClient() (*mtproto.Client, bool) {
	mtprotoMu.RLock()
	defer mtprotoMu.RUnlock()
	if mtprotoClient != nil && mtprotoClient.IsReady() {
		return mtprotoClient, true
	}
	return nil, false
}

// Send отправляет сообщения в Telegram
func Send(ctx context.Context, parsedResult *msgparser.ParsedResult, headers map[string]string,
	tgmessages *tgmessage.TgMessages, m *metrics.Metrics, buttonScheduler *buttonscheduler.ButtonScheduler) {

	initMTProto()

	chatID := os.Getenv("TG_CHAT_ID")
	commentID, _ := strconv.Atoi(headers["comment_id"])

	// Ждем готовности клиента (не более 30 сек)
	var client *mtproto.Client
	var ready bool
	if os.Getenv("TG_WS_PROXY_ENABLED") == "true" {
		log.Printf("⏳ Waiting for MTProto client to be ready...")
		for i := 0; i < 60; i++ {
			client, ready = getClient()
			if ready {
				log.Printf("✅ MTProto client ready, proceeding with send")
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	useMTProto := ready && client != nil

	// Если клиент готов и есть форматированное сообщение - отправляем через MTProto
	if useMTProto && parsedResult.Formatted != nil {
		log.Printf("🚀 Sending formatted message via MTProto")

		var messageID int32
		var sendErr error

		for attempt := 1; attempt <= 100; attempt++ {
			// Перед каждой попыткой убеждаемся, что клиент жив
			client, ready = getClient()
			if !ready {
				log.Printf("⚠️ Client lost, waiting for reconnect...")
				time.Sleep(3 * time.Second)
				continue
			}

			var id int
			var err error
			if buttonScheduler.ShouldShowButton() {
				qurl, _ := cleanQuestionURL(headers["comment_link"])
				id, err = client.SendFormattedMessageWithButton(ctx, chatID,
					parsedResult.Formatted, "Подключайтесь к соборному интеллекту", qurl)
				buttonScheduler.Reset()
			} else {
				id, err = client.SendFormattedMessage(ctx, chatID, parsedResult.Formatted)
			}

			if err != nil {
				sendErr = err
				log.Printf("MTProto send error (attempt %d/100): %v", attempt, err)
				sentry.CaptureMessage(fmt.Sprint(err))
				time.Sleep(5 * time.Second)
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

		m.MessagesSent.WithLabelValues().Inc()
		tgMessage := tgmessage.TgMessage{CommentID: commentID, MessageID: messageID}
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

// cleanQuestionURL очищает URL от фрагментов и кодирует пробелы
func cleanQuestionURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsedURL.Fragment != "" {
		parsedURL.Fragment = strings.ReplaceAll(parsedURL.Fragment, " ", "%20")
	}
	return parsedURL.String(), nil
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
