// Package telegramfilter предоставляет функции для фильтрации Telegram-упоминаний в тексте.
package telegramfilter

import (
	"regexp"
	"strings"
)

var (
	// Находим все упоминания, начинающиеся с @
	tgMentionRegex = regexp.MustCompile(`(^|\s)(@[a-zA-Z0-9_@]+)`)
)

// FilterMessage фильтрует текст сообщения, заменяя @ на _ во всех Telegram-упоминаниях
func FilterMessage(text string) string {
	// Обрабатываем все совпадения с регулярным выражением
	result := tgMentionRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Разделяем на пробел (если есть) и упоминание
		parts := strings.SplitN(match, "@", 2)
		if len(parts) < 2 {
			return match
		}

		prefix := parts[0]
		mention := "@" + parts[1]

		// Заменяем все @ в упоминании на _
		cleaned := strings.ReplaceAll(mention, "@", "_")
		return prefix + cleaned
	})

	return result
}
