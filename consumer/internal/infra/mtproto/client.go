package mtproto

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"golang.org/x/net/proxy"
)

type MTProtoClient struct {
	client *telegram.Client
	api    *tg.Client
	sender *message.Sender
	mu     sync.RWMutex
	ready  bool
	token  string
	cancel context.CancelFunc
}

// getAppCredentials возвращает appID и appHash из переменных окружения
// или дефолтные тестовые значения, если переменные не установлены
func getAppCredentials() (int, string) {
	// Дефолтные тестовые значения из примеров gotd/td
	defaultAppID := 2040
	defaultAppHash := "b18441a1ff607e10a989891a5462e627"

	appID := defaultAppID
	appHash := defaultAppHash

	// Читаем AppID из переменной окружения
	if envAppID := os.Getenv("TG_APP_ID"); envAppID != "" {
		if id, err := strconv.Atoi(envAppID); err == nil {
			appID = id
			log.Printf("📱 Using custom TG_APP_ID: %d", appID)
		} else {
			log.Printf("⚠️ Invalid TG_APP_ID '%s', using default: %d", envAppID, defaultAppID)
		}
	} else {
		log.Printf("📱 Using default TG_APP_ID: %d", appID)
	}

	// Читаем AppHash из переменной окружения
	if envAppHash := os.Getenv("TG_APP_HASH"); envAppHash != "" {
		appHash = envAppHash
		log.Printf("🔑 Using custom TG_APP_HASH: %s...", appHash[:8])
	} else {
		log.Printf("🔑 Using default TG_APP_HASH")
	}

	return appID, appHash
}

// getBotToken возвращает токен бота из переменной окружения или файла
func getBotToken() (string, error) {
	// Сначала пробуем прочитать из переменной окружения
	if envToken := os.Getenv("TG_BOT_TOKEN"); envToken != "" {
		return strings.TrimSpace(envToken), nil
	}

	// Затем пробуем прочитать из файла
	tokenFile := os.Getenv("TG_BOT_TOKEN_FILE")
	if tokenFile == "" {
		return "", fmt.Errorf("TG_BOT_TOKEN or TG_BOT_TOKEN_FILE must be set")
	}

	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("failed to read token file: %w", err)
	}

	return strings.TrimSpace(string(contents)), nil
}

func NewMTProtoClient(ctx context.Context) (*MTProtoClient, error) {
	// Получаем токен бота
	token, err := getBotToken()
	if err != nil {
		return nil, err
	}

	// Получаем appID и appHash
	appID, appHash := getAppCredentials()

	var resolver dcs.Resolver

	// Настраиваем прокси если включен
	if os.Getenv("TG_WS_PROXY_ENABLED") == "true" {
		proxyAddr := os.Getenv("TG_WS_PROXY_ADDR")
		if proxyAddr == "" {
			proxyAddr = "tg-ws-proxy:1443"
		}

		// Создаем SOCKS5 dialer для подключения к прокси
		socksDialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
		}

		// Используем Plain резолвер с кастомным dialer
		resolver = dcs.Plain(dcs.PlainOptions{
			Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return socksDialer.Dial(network, addr)
			},
			Rand: rand.Reader,
		})

		log.Printf("🔄 MTProto proxy enabled: %s", proxyAddr)
	} else {
		// Используем стандартный резолвер
		resolver = dcs.Plain(dcs.PlainOptions{
			Rand: rand.Reader,
		})
		log.Printf("📡 Direct MTProto connection (no proxy)")
	}

	// Создаем клиент
	client := telegram.NewClient(
		appID,
		appHash,
		telegram.Options{
			Resolver: resolver,
		},
	)

	return &MTProtoClient{
		client: client,
		token:  token,
	}, nil
}

func (c *MTProtoClient) Connect(parentCtx context.Context) error {
	ctx, cancel := context.WithCancel(parentCtx)
	c.cancel = cancel

	return c.client.Run(ctx, func(ctx context.Context) error {
		// Проверяем статус авторизации
		status, err := c.client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("auth status failed: %w", err)
		}

		if !status.Authorized {
			// Авторизуемся как бот
			if _, err := c.client.Auth().Bot(ctx, c.token); err != nil {
				return fmt.Errorf("bot auth failed: %w", err)
			}
			log.Printf("✅ Bot authenticated via MTProto")
		}

		c.api = c.client.API()
		c.sender = message.NewSender(c.api)

		c.mu.Lock()
		c.ready = true
		c.mu.Unlock()

		log.Printf("✅ MTProto client ready")

		// Держим соединение открытым
		<-ctx.Done()
		return ctx.Err()
	})
}

func (c *MTProtoClient) SendMessage(ctx context.Context, chatID string, text string) (int, error) {
	c.mu.RLock()
	ready := c.ready
	c.mu.RUnlock()

	if !ready {
		return 0, fmt.Errorf("client not ready")
	}

	// Резолвим peer
	peer, err := c.resolvePeer(ctx, chatID)
	if err != nil {
		return 0, fmt.Errorf("resolve peer failed: %w", err)
	}

	// Отправляем сообщение
	req := c.sender.To(peer)
	result, err := req.Text(ctx, text)
	if err != nil {
		return 0, fmt.Errorf("send message failed: %w", err)
	}

	// Извлекаем ID сообщения из ответа
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

func (c *MTProtoClient) resolvePeer(ctx context.Context, chatID string) (tg.InputPeerClass, error) {
	// Если chatID начинается с @, это username
	if len(chatID) > 0 && chatID[0] == '@' {
		username := chatID[1:]
		resolved, err := c.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
			Username: username,
		})
		if err != nil {
			return nil, err
		}

		if len(resolved.Chats) > 0 {
			chat := resolved.Chats[0]
			if channel, ok := chat.(*tg.Channel); ok {
				return &tg.InputPeerChannel{
					ChannelID:  channel.ID,
					AccessHash: channel.AccessHash,
				}, nil
			}
		}

		return nil, fmt.Errorf("chat not found: %s", chatID)
	}

	// Если это числовой ID
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid chat ID: %w", err)
	}

	// Пробуем использовать как ID канала без хеша
	return &tg.InputPeerChannel{
		ChannelID: id,
	}, nil
}

func (c *MTProtoClient) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

func (c *MTProtoClient) Close() {
	if c.cancel != nil {
		c.cancel()
	}
}
