package linkprocessor

import (
	"fmt"
	"strings"
)

// Правило для обработки ссылок
type LinkRule struct {
	Contains  string // Строка, которая должна содержаться в ссылке
	Replace   string // Строка, которую нужно заменить (например, "https://")
	Exception string // Исключение (если ссылка содержит эту строку, она не обрабатывается)
}

// TgLinkClipper обрабатывает ссылки по заданным правилам
func TgLinkClipper(link string) string {
	// Список правил для обработки ссылок
	rules := []LinkRule{
		{
			Contains:  "https://t.me",
			Replace:   "https://",
			Exception: "https://t.me/svoddru", // Исключение для канала svoddru
		},
		{
			Contains:  "http://t.me",
			Replace:   "http://",
			Exception: "",
		},
		{
			Contains:  "https://dzen.ru",
			Replace:   "https://",
			Exception: "",
		},
		{
			Contains:  "http://dzen.ru",
			Replace:   "http://",
			Exception: "",
		},
	}

	// Применение правил
	for _, rule := range rules {
		// Если ссылка содержит исключение, пропускаем обработку
		if rule.Exception != "" && strings.Contains(link, rule.Exception) {
			continue
		}

		// Если ссылка содержит указанную строку, применяем правило
		if strings.Contains(link, rule.Contains) {
			link = strings.ReplaceAll(link, rule.Replace, "")
			link = fmt.Sprintf("_%v", link)
		}
	}

	return link
}
