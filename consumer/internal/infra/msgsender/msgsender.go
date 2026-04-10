package msgsender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/domain/message"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/buttonscheduler"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/metrics"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/repos/tgmessage"
)

// HTTPError represents an HTTP error returned by a server.
type HTTPError struct {
	StatusCode int
	Status     string
}

// Error returns a string representation of the HTTP error.
func (err HTTPError) Error() string {
	return fmt.Sprintf("failed to send successful request. Status was %s", err.Status)
}

type Msgresponse struct {
	Ok     bool
	Result map[string]interface{}
}

// createHTTPClient создает HTTP клиент с поддержкой прокси или без
func createHTTPClient() *http.Client {
	// Если прокси выключен, возвращаем обычный клиент
	if os.Getenv("TG_WS_PROXY_ENABLED") != "true" {
		return &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	proxyAddr := os.Getenv("TG_WS_PROXY_ADDR")
	if proxyAddr == "" {
		proxyAddr = "tg-ws-proxy:1443"
	}

	log.Printf("🔄 Using proxy: %s", proxyAddr)

	// Создаем транспорт с кастомным dialer для подключения через прокси
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Подключаемся напрямую к прокси
			conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to proxy %s: %w", proxyAddr, err)
			}
			return conn, nil
		},
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second, // Увеличиваем таймаут для прокси
	}
}

func Send(ctx context.Context, messages []string, headers map[string]string, tgmessages *tgmessage.TgMessages,
	m *metrics.Metrics, buttonScheduler *buttonscheduler.ButtonScheduler) {

	contents, _ := os.ReadFile(os.Getenv("TG_BOT_TOKEN_FILE"))
	token := fmt.Sprintf("%v", strings.Trim(string(contents), "\r\n"))

	tgUrl := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	chatID := os.Getenv("TG_CHAT_ID")

	// Парсим ID комментария
	commentID, err := strconv.Atoi(headers["comment_id"])
	if err != nil {
		log.Printf("can not parse comment_id %v", err)
		commentID = 0
	}

	// Создаем HTTP клиент (с прокси или без)
	httpClient := createHTTPClient()

	// Флаг для отслеживания, пробовали ли мы уже fallback
	proxyFailed := false

	for i, text := range messages {
		msg := &message.Message{
			ChatID:    chatID,
			Text:      text,
			ParseMode: "HTML",
		}

		// Проверяем, нужно ли показывать кнопку (только на последнем сообщении)
		if buttonScheduler.ShouldShowButton() && i == len(messages)-1 {
			qurl, err := cleanQuestionURL(headers["comment_link"])
			if err == nil {
				button := message.InlineButton{
					Text:         "Подключайтесь к соборному интеллекту",
					CallbackData: qurl, // Передаем URL в callback_data
					URL:          qurl,
				}

				inlineKeyboard := make([][]message.InlineButton, 1)
				inlineKeyboard[0] = append(inlineKeyboard[0], button)

				msg.ReplyMarkup = &message.ReplyMarkup{
					InlineKeyboard: inlineKeyboard,
				}
			}

			// Сбрасываем счетчик и задаем новый интервал после показа кнопки
			buttonScheduler.Reset()
		}

		// Текущий клиент для отправки
		currentClient := httpClient

		for attempt := 1; attempt <= 100; attempt++ {
			// Делаем 100 попыток отправки, если получена ошибка, если нет, то цикл сразу завершается
			messageID, err := sendMessageWithClient(currentClient, tgUrl, msg)
			if err != nil {
				cm := fmt.Sprintf("error: %v Text: %s", err, msg.Text)
				log.Println(cm)
				sentry.CaptureMessage(cm)

				// Если прокси включен и еще не пробовали fallback, пробуем без прокси
				if os.Getenv("TG_WS_PROXY_ENABLED") == "true" && !proxyFailed {
					log.Printf("⚠️ Proxy failed, trying direct connection...")
					proxyFailed = true
					currentClient = &http.Client{Timeout: 10 * time.Second}
					// Не увеличиваем attempt, чтобы не тратить попытки
					attempt--
					time.Sleep(time.Second * 5)
					continue
				}

				time.Sleep(time.Second * 5)
				continue
			}

			// Фиксируем метрику при отправке сообщения
			m.MessagesSent.WithLabelValues().Inc()

			// Формируем сообщение в БД
			tgMessage := tgmessage.TgMessage{
				CommentID: commentID,
				MessageID: *messageID,
			}

			// Сохраняем ID сообщения в БД
			err = tgmessages.Create(context.Background(), tgMessage)
			if err != nil {
				log.Printf("error create tgmessage ID: %v", err)
			} else {
				log.Printf("tgMessage: %+v", tgMessage)
			}

			break
		}

		// Ожидаем 3 секунды после отправки, необходимо для соблюдения лимитов отправки сообщений ботом, 20 сообщений в минуту
		time.Sleep(time.Second * 3)
	}
}

// sendMessageWithClient отправляет сообщение используя переданный HTTP клиент
func sendMessageWithClient(client *http.Client, url string, message *message.Message) (*int32, error) {
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}

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
		log.Printf("response body: %v", j)
		messageID = int32(j.Result["message_id"].(float64))
	}

	return &messageID, nil
}

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
