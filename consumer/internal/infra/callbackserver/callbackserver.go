package callbackserver

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/terratensor/tg-svodd-bot/consumer/internal/metrics"
)

// CallbackServer представляет сервер для обработки callback-запросов
type CallbackServer struct {
	server *http.Server
	m      *metrics.Metrics
}

// New создает новый экземпляр CallbackServer
func New(addr string, m *metrics.Metrics) *CallbackServer {
	return &CallbackServer{
		server: &http.Server{
			Addr: addr,
		},
		m: m,
	}
}

// Start запускает сервер и настраивает Graceful Shutdown
func (cs *CallbackServer) Start() {
	// Настраиваем обработчик для callback-запросов
	http.HandleFunc("/callback", cs.handleCallback)

	// Запускаем сервер в отдельной горутине
	go func() {
		log.Printf("Starting callback server on %s", cs.server.Addr)
		if err := cs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start callback server: %v", err)
		}
	}()

	// Настраиваем Graceful Shutdown
	cs.setupGracefulShutdown()
}

// handleCallback обрабатывает callback-запросы от Telegram
func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	var update struct {
		CallbackQuery struct {
			ID   string `json:"id"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Message struct {
				MessageID int `json:"message_id"`
			} `json:"message"`
			Data string `json:"data"` // callback_data
		} `json:"callback_query"`
	}

	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("Failed to decode callback query: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Увеличиваем счетчик нажатий на кнопку
	cs.m.ButtonClicks.WithLabelValues().Inc()
	log.Printf("🚩🚩🚩 Button clicked by user %d", update.CallbackQuery.From.ID)

	// Извлекаем URL из callback_data
	redirectURL := update.CallbackQuery.Data

	// Отправляем ответ на callback-запрос
	response := map[string]interface{}{
		"method":            "answerCallbackQuery",
		"callback_query_id": update.CallbackQuery.ID,
		"url":               redirectURL, // Используем URL из callback_data
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// setupGracefulShutdown настраивает Graceful Shutdown для сервера
func (cs *CallbackServer) setupGracefulShutdown() {
	// Канал для получения сигналов завершения
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Ожидаем сигнал завершения
	<-stop

	// Создаем контекст с таймаутом для завершения работы сервера
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Останавливаем сервер
	log.Println("Shutting down callback server...")
	if err := cs.server.Shutdown(ctx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	} else {
		log.Println("Callback server stopped gracefully")
	}
}
