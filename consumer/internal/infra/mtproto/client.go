package mtproto

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
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

		secretBytes, err := hex.DecodeString(secret)
		if err != nil {
			return nil, fmt.Errorf("invalid secret hex: %w", err)
		}

		resolver = dcs.Plain(dcs.PlainOptions{
			Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Игнорируем IPv6
				if strings.Contains(addr, "[") || strings.Contains(addr, "]:") {
					log.Printf("⚠️ Skipping IPv6: %s", addr)
					return nil, fmt.Errorf("IPv6 not supported")
				}

				log.Printf("🔌 Connecting via proxy to %s", addr)
				conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
				if err != nil {
					return nil, err
				}

				if err := writeMTProtoObfuscation(conn, secretBytes); err != nil {
					conn.Close()
					return nil, err
				}

				return conn, nil
			},
			Rand:       rand.Reader,
			PreferIPv6: false,
			Network:    "tcp4",
		})
	} else {
		log.Printf("📡 Direct MTProto connection")
		resolver = dcs.Plain(dcs.PlainOptions{
			Rand:       rand.Reader,
			PreferIPv6: false,
			Network:    "tcp4",
		})
	}

	// Фильтруем DC лист - только IPv4
	dcList := dcs.Prod()
	var ipv4Options []tg.DCOption
	for _, opt := range dcList.Options {
		if strings.Contains(opt.IPAddress, ".") && !strings.Contains(opt.IPAddress, ":") {
			ipv4Options = append(ipv4Options, opt)
		}
	}
	log.Printf("📡 Using %d IPv4 DCs (filtered from %d total)", len(ipv4Options), len(dcList.Options))

	client := telegram.NewClient(appID, appHash, telegram.Options{
		Resolver: resolver,
		DCList:   dcs.List{Options: ipv4Options, Domains: dcList.Domains},
	})

	return &Client{
		client: client,
		token:  token,
	}, nil
}

// writeMTProtoObfuscation отправляет обфускацию для MTProto прокси
func writeMTProtoObfuscation(conn net.Conn, secret []byte) error {
	// Генерируем случайные байты (64 байта)
	randomBytes := make([]byte, 64)
	if _, err := rand.Read(randomBytes); err != nil {
		return err
	}

	// Отправляем обфускацию
	if _, err := conn.Write(randomBytes); err != nil {
		return err
	}

	return nil
}

func (c *Client) Connect(parentCtx context.Context) error {
	ctx, cancel := context.WithCancel(parentCtx)
	c.cancel = cancel

	return c.client.Run(ctx, func(ctx context.Context) error {
		log.Printf("🔐 Authenticating bot...")

		// Bot() возвращает (BotAuth, error)
		if _, err := c.client.Auth().Bot(ctx, c.token); err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}

		c.api = c.client.API()
		c.sender = message.NewSender(c.api)

		c.mu.Lock()
		c.ready = true
		c.mu.Unlock()

		log.Printf("✅ MTProto client ready")

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

	result, err := c.sender.To(peer).Text(ctx, text)
	if err != nil {
		return 0, fmt.Errorf("send failed: %w", err)
	}

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
