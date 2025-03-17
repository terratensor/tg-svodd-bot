package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/db/pgstore"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/buttonscheduler"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/msghandler"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/msgreceiver"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/lib/secret"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/metrics"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/repos/tgmessage"

	_ "gocloud.dev/pubsub/rabbitpubsub"
)

func main() {
	initializeTimezone()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	mode := os.Getenv("APP_ENV")

	if mode == "prod" {
		contents, err := os.ReadFile(os.Getenv("SENTRY_DSN_FILE"))
		if err != nil {
			log.Fatalf("can not read SENTRY_DSN_FILE")
		}
		dsn := fmt.Sprintf("%v", strings.Trim(string(contents), "\r\n"))

		err = sentry.Init(sentry.ClientOptions{
			Dsn: dsn,
			// Set TracesSampleRate to 1.0 to capture 100%
			// of transactions for performance monitoring.
			// We recommend adjusting this value in production,
			TracesSampleRate: 1.0,
		})
		if err != nil {
			log.Fatalf("sentry.Init: %s", err)
		}
	}

	// Создаем метрики
	m := metrics.NewMetrics()
	m.Register()

	// Запускаем сервер для метрик и callback-запросов
	startServer(m)

	// Подготавливаем подключение к БД
	dsn := newDBConnectionString()
	pgst, err := pgstore.NewMessages(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pgst.Close()

	tgmessages := tgmessage.NewTgMessages(pgst)

	// Создаем планировщик кнопок
	buttonScheduler := buttonscheduler.NewButtonScheduler()

	// Подготавливаем канал для обработки комментариев
	ch := make(chan msghandler.Request, 100)
	wg := &sync.WaitGroup{}
	wg.Add(2)

	go func() {
		err := msgreceiver.Run(ctx, ch, wg)
		// Обрабатываем ошибку и выходим с кодом 1, для того чтобы инициировать перезапуск докер контейнера.
		// Возможно тут имеет смысл сделать сервис проверки health, но пока так
		if err != nil {
			log.Printf("%v\r\n failure, restart required", err)
			sentry.CaptureMessage(fmt.Sprint(err))
			os.Exit(1)
		}
	}()

	go msghandler.Handler(ctx, ch, wg, tgmessages, m, buttonScheduler)

	if mode == "PROD" {
		// Flush buffered events before the program terminates.
		defer sentry.Flush(2 * time.Second)
	}

	wg.Wait()
	stop()
}

func newDBConnectionString() string {
	dbUser := os.Getenv("POSTGRES_USER")
	dbPassword := strings.TrimRight(string(secret.Read(os.Getenv("POSTGRES_PASSWORD_FILE"))), "\r\n")
	dbName := os.Getenv("POSTGRES_DB")
	dbHost := os.Getenv("POSTGRES_HOST")

	return fmt.Sprintf("postgresql://%s:%s@%s/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbName)
}

func initializeTimezone() {
	if timezone := os.Getenv("TZ"); timezone != "" {
		if location, err := time.LoadLocation(timezone); err != nil {
			log.Printf("error loading timezone '%s': %v\n", timezone, err)
		} else {
			time.Local = location
		}
	}

	now := time.Now()
	log.Printf("Local timezone: %s. Service started at %s", time.Local.String(),
		now.Format("2006-01-02T15:04:05.000 MST"))
}

// startServer запускает HTTP-сервер для экспорта метрик и обработки callback-запросов
func startServer(m *metrics.Metrics) {
	// Маршрут для метрик
	http.Handle("/metrics", promhttp.Handler())

	// Маршрут для обработки callback-запросов
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		var update struct {
			CallbackQuery struct {
				ID   string `json:"id"`
				From struct {
					ID int64 `json:"id"`
				} `json:"from"`
				Message struct {
					MessageID int `json:"message_id"`
				} `json:"message"`
				Data string `json:"data"` // Данные из callback_data
			} `json:"callback_query"`
		}

		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			log.Printf("Failed to decode callback query: %v", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Извлекаем URL из callback_data
		var callbackData map[string]string
		if err := json.Unmarshal([]byte(update.CallbackQuery.Data), &callbackData); err != nil {
			log.Printf("Failed to unmarshal callback data: %v", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		url, ok := callbackData["url"]
		if !ok {
			log.Printf("URL not found in callback data")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Увеличиваем счетчик нажатий на кнопку
		m.ButtonClicks.WithLabelValues().Inc()

		// Отправляем ответ на callback-запрос
		response := map[string]interface{}{
			"method":            "answerCallbackQuery",
			"callback_query_id": update.CallbackQuery.ID,
			"url":               url, // Используем URL из callback_data
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	// Запускаем сервер
	go func() {
		log.Printf("Starting server on :8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()
}
