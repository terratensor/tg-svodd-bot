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
	mtprotoMu         sync.RWMutex
)

// InitMTProto глобальная инициализация (вызывается из main)
func InitMTProto() {
	if os.Getenv("TG_WS_PROXY_ENABLED") != "true" {
		log.Printf("📡 MTProto disabled")
		return
	}

	mtprotoClientOnce.Do(func() {
		mtprotoCtx, mtprotoCancel = context.WithCancel(context.Background())
		go mtprotoConnectionManager(mtprotoCtx)
	})
}

// ShutdownMTProto закрывает MTProto клиент (вызывается при завершении)
func ShutdownMTProto() {
	if mtprotoCancel != nil {
		mtprotoCancel()
	}
	if mtprotoClient != nil {
		mtprotoClient.Close()
	}
}

// mtprotoConnectionManager управляет подключением с повторными попытками
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

		mtprotoMu.Lock()
		mtprotoClient = client
		mtprotoReady = true
		mtprotoMu.Unlock()

		log.Printf("✅ MTProto client connected and ready")
		retryDelay = 5 * time.Second

		// Просто ждем пока контекст не отменят
		<-ctx.Done()

		mtprotoMu.Lock()
		mtprotoReady = false
		mtprotoMu.Unlock()
		return
	}
}

// getMTProtoClient возвращает текущего клиента
func getMTProtoClient() (*mtproto.Client, bool) {
	mtprotoMu.RLock()
	defer mtprotoMu.RUnlock()

	log.Printf("DEBUG: mtprotoReady=%v, client=%v", mtprotoReady, mtprotoClient != nil)
	if mtprotoClient != nil {
		log.Printf("DEBUG: client.IsReady()=%v", mtprotoClient.IsReady())
	}

	return mtprotoClient, mtprotoReady && mtprotoClient != nil && mtprotoClient.IsReady()
}

// Send отправляет сообщения в Telegram
func Send(ctx context.Context, parsedResult *msgparser.ParsedResult, headers map[string]string,
	tgmessages *tgmessage.TgMessages, m *metrics.Metrics, buttonScheduler *buttonscheduler.ButtonScheduler) {

	chatID := os.Getenv("TG_CHAT_ID")

	commentID, err := strconv.Atoi(headers["comment_id"])
	if err != nil {
		log.Printf("can not parse comment_id %v", err)
		commentID = 0
	}

	useMTProto := os.Getenv("TG_WS_PROXY_ENABLED") == "true"
	var client *mtproto.Client
	var ready bool

	if useMTProto {
		log.Printf("⏳ Waiting for MTProto client to be ready...")
		for i := 0; i < 60; i++ {
			client, ready = getMTProtoClient()
			if ready {
				log.Printf("✅ MTProto client ready, proceeding with send")
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !ready {
			log.Printf("❌ MTProto client not ready, falling back to HTML")
			useMTProto = false
		}
	}

	if useMTProto && parsedResult.Formatted != nil && client != nil {
		log.Printf("🚀 Sending formatted message via MTProto")

		var messageID int32
		var sendErr error

		for attempt := 1; attempt <= 100; attempt++ {
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
				cm := fmt.Sprintf("MTProto send error (attempt %d/100): %v", attempt, err)
				log.Println(cm)
				sentry.CaptureMessage(cm)

				if !client.IsReady() {
					log.Printf("⚠️ Client died, will retry after reconnect")
					mtprotoMu.Lock()
					mtprotoReady = false
					mtprotoMu.Unlock()
					time.Sleep(3 * time.Second)
					client, ready = getMTProtoClient()
					if !ready {
						log.Printf("❌ Still not ready, falling back to HTML")
						useMTProto = false
						break
					}
				}
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

	log.Printf("📡 Falling back to HTML messages")
	messages := parsedResult.Messages

	for i, text := range messages {
		msgText := text

		shouldShowButton := buttonScheduler.ShouldShowButton() && i == len(messages)-1
		if shouldShowButton {
			buttonScheduler.Reset()
		}

		var messageID int32
		var sendErr error

		for attempt := 1; attempt <= 100; attempt++ {
			if useMTProto && client != nil {
				id, err := client.SendMessage(ctx, chatID, msgText)
				if err != nil {
					sendErr = err
					log.Printf("MTProto send error (attempt %d/100): %v", attempt, err)
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

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func cleanQuestionURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("ошибка при разборе URL: %v", err)
	}
	parsedURL.Fragment = ""
	return parsedURL.String(), nil
}
