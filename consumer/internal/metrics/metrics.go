package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics структура для хранения всех метрик
type Metrics struct {
	MessagesSent       *prometheus.CounterVec
	SpamMessagesMarked *prometheus.CounterVec
	MessagesBlocked    *prometheus.CounterVec
	ButtonClicks       *prometheus.CounterVec
}

// NewMetrics создает и возвращает новый экземпляр Metrics
func NewMetrics() *Metrics {
	return &Metrics{
		MessagesSent: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tg_svodd_bot_messages_sent_total",
				Help: "Total number of messages sent by the bot.",
			},
			[]string{}, // Метки, если нужны
		),
		SpamMessagesMarked: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tg_svodd_bot_spam_messages_marked_total",
				Help: "Total number of messages marked as spam.",
			},
			[]string{}, // Метки, если нужны
		),
		MessagesBlocked: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tg_svodd_bot_messages_blocked_total",
				Help: "Total number of messages blocked by the bot.",
			},
			[]string{}, // Метки, если нужны
		),
		ButtonClicks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tg_svodd_bot_button_clicks_total",
				Help: "Total number of button clicks.",
			},
			[]string{}, // Метки, если нужны
		),
	}
}

// Register регистрирует метрики в Prometheus
func (m *Metrics) Register() {
	prometheus.MustRegister(m.MessagesSent)
	prometheus.MustRegister(m.SpamMessagesMarked)
	prometheus.MustRegister(m.MessagesBlocked)
	prometheus.MustRegister(m.ButtonClicks)
}
