package proxy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// TelegramProxyClient - клиент для работы с tg-ws-proxy
type TelegramProxyClient struct {
	proxyAddr  string
	secret     string
	enabled    bool
	httpClient *http.Client
}

// NewTelegramProxyClient создает новый клиент прокси
func NewTelegramProxyClient() *TelegramProxyClient {
	enabled := os.Getenv("TG_WS_PROXY_ENABLED") == "true"
	proxyAddr := os.Getenv("TG_WS_PROXY_ADDR")
	if proxyAddr == "" {
		proxyAddr = "tg-ws-proxy:1443"
	}

	secret := os.Getenv("TG_WS_PROXY_SECRET")

	client := &TelegramProxyClient{
		proxyAddr: proxyAddr,
		secret:    secret,
		enabled:   enabled,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	if enabled {
		log.Printf("🔄 Telegram Proxy enabled: %s", proxyAddr)
	} else {
		log.Printf("📡 Telegram Proxy disabled, using direct API")
	}

	return client
}

// IsEnabled возвращает true если прокси включен
func (c *TelegramProxyClient) IsEnabled() bool {
	return c.enabled
}

// SendMessage отправляет сообщение через прокси или напрямую
func (c *TelegramProxyClient) SendMessage(ctx context.Context, token string, payload []byte) (*http.Response, error) {
	if !c.enabled {
		// Fallback к прямому запросу
		return c.sendDirect(ctx, token, payload)
	}

	// Пробуем отправить через прокси
	resp, err := c.sendViaProxy(ctx, token, payload)
	if err != nil {
		log.Printf("⚠️ Proxy request failed: %v, falling back to direct API", err)
		// Fallback к прямому запросу
		return c.sendDirect(ctx, token, payload)
	}

	return resp, nil
}

// sendViaProxy отправляет запрос через tg-ws-proxy
func (c *TelegramProxyClient) sendViaProxy(ctx context.Context, token string, payload []byte) (*http.Response, error) {
	// Формируем URL для прокси
	proxyURL := fmt.Sprintf("http://%s/bot%s/sendMessage", c.proxyAddr, token)

	req, err := http.NewRequestWithContext(ctx, "POST", proxyURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Добавляем заголовок с секретом для аутентификации
	if c.secret != "" {
		req.Header.Set("X-Proxy-Secret", c.generateAuthHeader())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy request failed: %w", err)
	}

	log.Printf("✅ Message sent via proxy")
	return resp, nil
}

// sendDirect отправляет запрос напрямую в Telegram API
func (c *TelegramProxyClient) sendDirect(ctx context.Context, token string, payload []byte) (*http.Response, error) {
	directURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)

	req, err := http.NewRequestWithContext(ctx, "POST", directURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create direct request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("direct request failed: %w", err)
	}

	log.Printf("📡 Message sent directly to Telegram API")
	return resp, nil
}

// generateAuthHeader генерирует заголовок аутентификации для прокси
func (c *TelegramProxyClient) generateAuthHeader() string {
	// Создаем HMAC-SHA256 от секрета
	h := hmac.New(sha256.New, []byte(c.secret))
	h.Write([]byte("tg-ws-proxy"))
	return hex.EncodeToString(h.Sum(nil))
}

// HealthCheck проверяет доступность прокси
func (c *TelegramProxyClient) HealthCheck(ctx context.Context) error {
	if !c.enabled {
		return nil
	}

	healthURL := fmt.Sprintf("http://%s/health", c.proxyAddr)
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy health check failed: %s", resp.Status)
	}

	return nil
}
