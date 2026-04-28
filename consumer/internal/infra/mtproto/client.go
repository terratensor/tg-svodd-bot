package mtproto

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"

	// Импортируем наш пакет с типами
	domainMsg "github.com/terratensor/tg-svodd-bot/consumer/internal/domain/message"
)

type Client struct {
	client *telegram.Client
	api    *tg.Client
	sender *message.Sender
	mu     sync.RWMutex
	ready  bool
	token  string
	cancel context.CancelFunc
}

func New(ctx context.Context) (*Client, error) {
	token, err := getToken()
	if err != nil {
		return nil, err
	}

	appID, appHash := getCredentials()

	var resolver dcs.Resolver

	// Настраиваем MTProto прокси если включен
	if os.Getenv("TG_WS_PROXY_ENABLED") == "true" {
		proxyAddr := os.Getenv("TG_WS_PROXY_ADDR")
		if proxyAddr == "" {
			proxyAddr = "tg-ws-proxy:1443"
		}

		secret := os.Getenv("TG_WS_PROXY_SECRET")
		if secret == "" {
			return nil, fmt.Errorf("TG_WS_PROXY_SECRET is required")
		}

		log.Printf("🔄 MTProto proxy enabled: %s", proxyAddr)

		// Декодируем секрет из hex
		secretBytes, err := hex.DecodeString(secret)
		if err != nil {
			return nil, fmt.Errorf("invalid secret hex: %w", err)
		}

		// dcs.MTProxy возвращает (Resolver, error)
		mtProxy, err := dcs.MTProxy(proxyAddr, secretBytes, dcs.MTProxyOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create MTProxy resolver: %w", err)
		}
		resolver = mtProxy

		log.Printf("✅ MTProto resolver configured")
	} else {
		log.Printf("📡 Direct MTProto connection")
		resolver = dcs.Plain(dcs.PlainOptions{})
	}

	client := telegram.NewClient(appID, appHash, telegram.Options{
		Resolver: resolver,
		DC:       2, // Основной DC для ботов
	})

	return &Client{
		client: client,
		token:  token,
	}, nil
}

func (c *Client) Connect(parentCtx context.Context) error {
	ctx, cancel := context.WithCancel(parentCtx)
	c.cancel = cancel

	return c.client.Run(ctx, func(ctx context.Context) error {
		start := time.Now()
		log.Printf("🔐 MTProto Run started")

		// Проверяем статус до аутентификации
		statusStart := time.Now()
		status, err := c.client.Auth().Status(ctx)
		if err != nil {
			log.Printf("⚠️ Auth status check failed: %v", err)
		} else {
			log.Printf("📊 Auth status check took %v: authorized=%v", time.Since(statusStart), status.Authorized)
		}

		// Всегда пытаемся авторизоваться
		log.Printf("🔐 Starting bot authentication...")
		authStart := time.Now()
		if _, err := c.client.Auth().Bot(ctx, c.token); err != nil {
			log.Printf("❌ Auth failed after %v: %v", time.Since(authStart), err)
			return fmt.Errorf("auth failed: %w", err)
		}
		log.Printf("✅ Auth completed in %v", time.Since(authStart))

		apiStart := time.Now()
		c.api = c.client.API()
		c.sender = message.NewSender(c.api)
		log.Printf("📡 API initialization took %v", time.Since(apiStart))

		c.mu.Lock()
		c.ready = true
		c.mu.Unlock()

		log.Printf("✅ MTProto client fully ready in %v", time.Since(start))

		<-ctx.Done()
		return ctx.Err()
	})
}

func (c *Client) SendMessage(ctx context.Context, chatID string, text string) (int, error) {
	c.mu.RLock()
	ready := c.ready
	c.mu.RUnlock()

	if !ready {
		return 0, fmt.Errorf("client not ready")
	}

	peer, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return 0, fmt.Errorf("resolve peer failed: %w", err)
	}

	start := time.Now()
	result, err := c.sender.To(peer).Text(ctx, text)
	if err != nil {
		return 0, fmt.Errorf("send failed: %w", err)
	}
	log.Printf("📤 Message sent in %v", time.Since(start))

	// Обрабатываем разные типы ответов
	switch update := result.(type) {
	case *tg.UpdateShortSentMessage:
		return update.ID, nil
	case *tg.Updates:
		for _, upd := range update.Updates {
			if msgUpdate, ok := upd.(*tg.UpdateMessageID); ok {
				return msgUpdate.ID, nil
			}
			if newMsg, ok := upd.(*tg.UpdateNewMessage); ok {
				if msg, ok := newMsg.Message.(*tg.Message); ok {
					return msg.ID, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("unexpected response type: %T", result)
}

func (c *Client) resolvePeer(ctx context.Context, chatID string) (tg.InputPeerClass, error) {
	if len(chatID) > 0 && chatID[0] == '@' {
		username := chatID[1:]
		resolved, err := c.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
			Username: username,
		})
		if err != nil {
			return nil, err
		}
		if len(resolved.Chats) > 0 {
			if ch, ok := resolved.Chats[0].(*tg.Channel); ok {
				return &tg.InputPeerChannel{
					ChannelID:  ch.ID,
					AccessHash: ch.AccessHash,
				}, nil
			}
		}
		return nil, fmt.Errorf("chat not found: %s", chatID)
	}

	id, _ := strconv.ParseInt(chatID, 10, 64)
	return &tg.InputPeerChannel{ChannelID: id}, nil
}

func (c *Client) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

func (c *Client) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}

func getToken() (string, error) {
	if t := os.Getenv("TG_BOT_TOKEN"); t != "" {
		return strings.TrimSpace(t), nil
	}
	data, err := os.ReadFile(os.Getenv("TG_BOT_TOKEN_FILE"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func getCredentials() (int, string) {
	appID := 2040
	appHash := "b18441a1ff607e10a989891a5462e627"

	if id := os.Getenv("TG_APP_ID"); id != "" {
		if i, _ := strconv.Atoi(id); i > 0 {
			appID = i
		}
	}
	if hash := os.Getenv("TG_APP_HASH"); hash != "" {
		appHash = hash
	}

	log.Printf("📱 Using AppID: %d", appID)
	return appID, appHash
}

// SendFormattedMessage отправляет форматированное сообщение через MTProto
func (c *Client) SendFormattedMessage(ctx context.Context, chatID string, fm *domainMsg.FormattedMessage) (int, error) {
	c.mu.RLock()
	ready := c.ready
	c.mu.RUnlock()

	if !ready {
		return 0, fmt.Errorf("client not ready")
	}

	peer, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return 0, fmt.Errorf("resolve peer failed: %w", err)
	}

	// Конвертируем наши entities в формат Telegram API
	var tgEntities []tg.MessageEntityClass

	for _, entity := range fm.Entities {
		switch entity.Type {
		case domainMsg.EntityItalic:
			tgEntities = append(tgEntities, &tg.MessageEntityItalic{
				Offset: entity.Offset,
				Length: entity.Length,
			})
		case domainMsg.EntityBold:
			tgEntities = append(tgEntities, &tg.MessageEntityBold{
				Offset: entity.Offset,
				Length: entity.Length,
			})
		case domainMsg.EntityTextURL:
			tgEntities = append(tgEntities, &tg.MessageEntityTextURL{
				Offset: entity.Offset,
				Length: entity.Length,
				URL:    entity.URL,
			})
		case domainMsg.EntityBlockquote:
			tgEntities = append(tgEntities, &tg.MessageEntityBlockquote{
				Offset: entity.Offset,
				Length: entity.Length,
			})
		}
	}

	// Формируем итоговый текст
	text := fm.Text

	// Добавляем подпись с источником
	// ... конвертация fm.Entities в tgEntities ...
	text = fm.AddSignatureEntity(text, &tgEntities)

	// ВАЛИДАЦИЯ
	if fm.Signature != nil {
		log.Printf("=== VALIDATION ===")
		log.Printf("Text length: %d", utf8.RuneCountInString(text))
		for i, entity := range tgEntities {
			if textURL, ok := entity.(*tg.MessageEntityTextURL); ok {
				start := textURL.Offset
				end := start + textURL.Length
				log.Printf("Entity[%d]: offset=%d, length=%d, end=%d", i, start, textURL.Length, end)
				if end <= utf8.RuneCountInString(text) {
					extracted := string([]rune(text)[start:end])
					log.Printf("Extracted text: '%s'", extracted)
					log.Printf("Expected text: '%s'", fm.Signature.Text)
					if extracted != fm.Signature.Text {
						log.Printf("❌ MISMATCH!")
					}
				} else {
					log.Printf("❌ Entity out of bounds!")
				}
			}
		}
	}

	// ========== ЛОГИ ДЛЯ ОТЛАДКИ ENTITIES ==========
	log.Printf("🔍 [SendFormattedMessage] Final text length: %d runes", utf8.RuneCountInString(text))
	log.Printf("🔍 [SendFormattedMessage] Full text: %s", text)
	for i, entity := range tgEntities {
		if textURL, ok := entity.(*tg.MessageEntityTextURL); ok {
			if textURL.URL == "" {
				log.Printf("⚠️ Warning: TextURL entity %d has empty URL", i)
			}
			if textURL.Length == 0 {
				log.Printf("⚠️ Warning: TextURL entity %d has zero length", i)
			}
			log.Printf("📎 TextURL entity %d: offset=%d, length=%d, url=%s",
				i, textURL.Offset, textURL.Length, textURL.URL)
		}
		switch e := entity.(type) {
		case *tg.MessageEntityBlockquote:
			log.Printf("🔍 [SendFormattedMessage] Entity[%d] BLOCKQUOTE: offset=%d, length=%d", i, e.Offset, e.Length)
		case *tg.MessageEntityTextURL:
			log.Printf("🔍 [SendFormattedMessage] Entity[%d] TEXT_URL: offset=%d, length=%d, url=%s", i, e.Offset, e.Length, e.URL)
		case *tg.MessageEntityItalic:
			log.Printf("🔍 [SendFormattedMessage] Entity[%d] ITALIC: offset=%d, length=%d", i, e.Offset, e.Length)
		default:
			log.Printf("🔍 [SendFormattedMessage] Entity[%d] OTHER: offset=%d, length=%d, type=%T",
				i, entity.GetOffset(), entity.GetLength(), entity)
		}
	}
	// ========== КОНЕЦ ЛОГОВ ==========

	// Отправляем через прямой API вызов
	request := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  text,
		Entities: tgEntities,
		RandomID: int64(time.Now().UnixNano()),
	}

	result, err := c.api.MessagesSendMessage(ctx, request)
	if err != nil {
		return 0, fmt.Errorf("send failed: %w", err)
	}

	return extractMessageID(result), nil
}

// SendFormattedMessageWithButton отправляет форматированное сообщение с инлайн кнопкой
func (c *Client) SendFormattedMessageWithButton(ctx context.Context, chatID string, fm *domainMsg.FormattedMessage, buttonText, buttonURL string) (int, error) {
	c.mu.RLock()
	ready := c.ready
	c.mu.RUnlock()

	if !ready {
		return 0, fmt.Errorf("client not ready")
	}

	peer, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return 0, fmt.Errorf("resolve peer failed: %w", err)
	}

	// Конвертируем наши entities в формат Telegram API
	var tgEntities []tg.MessageEntityClass

	for _, entity := range fm.Entities {
		switch entity.Type {
		case domainMsg.EntityItalic:
			tgEntities = append(tgEntities, &tg.MessageEntityItalic{
				Offset: entity.Offset,
				Length: entity.Length,
			})
		case domainMsg.EntityTextURL:
			tgEntities = append(tgEntities, &tg.MessageEntityTextURL{
				Offset: entity.Offset,
				Length: entity.Length,
				URL:    entity.URL,
			})
		case domainMsg.EntityBlockquote:
			tgEntities = append(tgEntities, &tg.MessageEntityBlockquote{
				Offset: entity.Offset,
				Length: entity.Length,
			})
		}
	}

	// Формируем итоговый текст
	text := fm.Text

	// Добавляем подпись с источником
	// ... конвертация fm.Entities в tgEntities ...
	text = fm.AddSignatureEntity(text, &tgEntities)

	// ВАЛИДАЦИЯ
	if fm.Signature != nil {
		log.Printf("=== VALIDATION ===")
		log.Printf("Text length: %d", utf8.RuneCountInString(text))
		for i, entity := range tgEntities {
			if textURL, ok := entity.(*tg.MessageEntityTextURL); ok {
				start := textURL.Offset
				end := start + textURL.Length
				log.Printf("Entity[%d]: offset=%d, length=%d, end=%d", i, start, textURL.Length, end)
				if end <= utf8.RuneCountInString(text) {
					extracted := string([]rune(text)[start:end])
					log.Printf("Extracted text: '%s'", extracted)
					log.Printf("Expected text: '%s'", fm.Signature.Text)
					if extracted != fm.Signature.Text {
						log.Printf("❌ MISMATCH!")
					}
				} else {
					log.Printf("❌ Entity out of bounds!")
				}
			}
		}
	}

	// Создаем инлайн клавиатуру с кнопкой
	replyMarkup := &tg.ReplyInlineMarkup{
		Rows: []tg.KeyboardButtonRow{
			{
				Buttons: []tg.KeyboardButtonClass{
					&tg.KeyboardButtonURL{
						Text: buttonText,
						URL:  buttonURL,
					},
				},
			},
		},
	}

	// ========== ЛОГИ ДЛЯ ОТЛАДКИ ENTITIES ==========
	log.Printf("🔍 [SendFormattedMessageWithButton] Final text length: %d runes", utf8.RuneCountInString(text))
	log.Printf("🔍 [SendFormattedMessageWithButton] Full text: %s", text)
	for i, entity := range tgEntities {
		if textURL, ok := entity.(*tg.MessageEntityTextURL); ok {
			if textURL.URL == "" {
				log.Printf("⚠️ Warning: TextURL entity %d has empty URL", i)
			}
			if textURL.Length == 0 {
				log.Printf("⚠️ Warning: TextURL entity %d has zero length", i)
			}
			log.Printf("📎 TextURL entity %d: offset=%d, length=%d, url=%s",
				i, textURL.Offset, textURL.Length, textURL.URL)
		}
		switch e := entity.(type) {
		case *tg.MessageEntityBlockquote:
			log.Printf("🔍 [SendFormattedMessageWithButton] Entity[%d] BLOCKQUOTE: offset=%d, length=%d", i, e.Offset, e.Length)
		case *tg.MessageEntityTextURL:
			log.Printf("🔍 [SendFormattedMessageWithButton] Entity[%d] TEXT_URL: offset=%d, length=%d, url=%s", i, e.Offset, e.Length, e.URL)
		case *tg.MessageEntityItalic:
			log.Printf("🔍 [SendFormattedMessageWithButton] Entity[%d] ITALIC: offset=%d, length=%d", i, e.Offset, e.Length)
		default:
			log.Printf("🔍 [SendFormattedMessageWithButton] Entity[%d] OTHER: offset=%d, length=%d, type=%T",
				i, entity.GetOffset(), entity.GetLength(), entity)
		}
	}
	// ========== КОНЕЦ ЛОГОВ ==========

	// Отправляем через прямой API вызов с клавиатурой
	request := &tg.MessagesSendMessageRequest{
		Peer:        peer,
		Message:     text,
		Entities:    tgEntities,
		ReplyMarkup: replyMarkup,
		RandomID:    int64(time.Now().UnixNano()),
	}

	result, err := c.api.MessagesSendMessage(ctx, request)
	if err != nil {
		return 0, fmt.Errorf("send failed: %w", err)
	}

	return extractMessageID(result), nil
}

// extractMessageID извлекает ID сообщения из ответа API
func extractMessageID(result tg.UpdatesClass) int {
	switch update := result.(type) {
	case *tg.UpdateShortSentMessage:
		return update.ID
	case *tg.Updates:
		for _, upd := range update.Updates {
			if msgUpdate, ok := upd.(*tg.UpdateMessageID); ok {
				return msgUpdate.ID
			}
			if newMsg, ok := upd.(*tg.UpdateNewMessage); ok {
				if msg, ok := newMsg.Message.(*tg.Message); ok {
					return msg.ID
				}
			}
		}
	}
	return 0
}
