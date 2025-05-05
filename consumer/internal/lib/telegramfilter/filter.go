// Package telegramfilter предоставляет функции для фильтрации Telegram-упоминаний и ссылок в тексте.
package telegramfilter

import (
	"regexp"
	"strings"
)

var (
	// Для упоминаний (@...)
	tgMentionRegex = regexp.MustCompile(`(^|\s)(@[a-zA-Z0-9_@]+)`)

	// Для ссылок (t.me/..., telegram.me/...)
	tgLinkRegex = regexp.MustCompile(`((t\.me|telegram\.me)/[a-zA-Z0-9_/]+)`)
)

// excludedDomains возвращает список доменов, которые не должны фильтроваться
func excludedDomains() []string {
	return []string{"t.me/svoddru", "telegram.me/svoddru"}
}

// excludedMentions возвращает список упоминаний, которые не должны фильтроваться
func excludedMentions() []string {
	return []string{"@svoddru"}
}

// FilterMessage фильтрует текст сообщения, заменяя:
// - Все @ на _ в Telegram-упоминаниях
// - Добавляет _ перед Telegram-ссылками (кроме исключений)
func FilterMessage(text string) string {
	// Сначала обрабатываем упоминания (@...)
	text = filterMentions(text)

	// Затем обрабатываем ссылки (t.me/..., telegram.me/...)
	text = filterLinks(text)

	return text
}

func filterMentions(text string) string {
	return tgMentionRegex.ReplaceAllStringFunc(text, func(match string) string {

		// Проверяем исключения
		for _, mention := range excludedMentions() {
			if strings.Contains(match, mention) {
				return match
			}
		}

		parts := strings.SplitN(match, "@", 2)
		if len(parts) < 2 {
			return match
		}

		prefix := parts[0]
		mention := "@" + parts[1]
		cleaned := strings.ReplaceAll(mention, "@", "_")
		return prefix + cleaned
	})
}

func filterLinks(text string) string {
	return tgLinkRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Проверяем исключения
		for _, domain := range excludedDomains() {
			if strings.Contains(match, domain) {
				return match
			}
		}

		// Добавляем _ перед ссылкой
		if strings.HasPrefix(match, " ") {
			return " _" + match[1:]
		}
		return "_" + match
	})
}
