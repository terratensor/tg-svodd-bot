package msgparser

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Parse обрабатывает текст сообщения для отправки в телеграм.
// Устанавливает необходимые html теги
func Parse(msg string) ([]string, error) {
	n, _ := html.Parse(strings.NewReader(msg))

	var builder strings.Builder
	var f func(*html.Node)

	// text := builder.String()

	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(html.EscapeString(n.Data))
		}
		if n.Type == html.TextNode && n.Data == "br" {
			builder.WriteString(fmt.Sprintf("\n%s", ""))
			return
		}
		if n.Type == html.ElementNode && n.Data == "blockquote" {
			builder.WriteString(fmt.Sprintf("\n%s\n", processBlockquote(n)))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)

	messages := splitMessage(builder.String(), 4096)

	return messages, nil
}

// splitMessage разбивает текст комментария на блоки размером не более чем 4096 символов
// разделитель для блоков \n
func splitMessage(msg string, chunkSize int) []string {
	var msgs []string
	if utf8.RuneCountInString(msg) < chunkSize {
		msgs = append(msgs, msg)
		return msgs
	}

	var builder strings.Builder
	chunks := strings.SplitAfter(msg, "\n")

	for _, chunk := range chunks {

		// Удаляем пробелы и если после этого chunk будет пустым то пропускаем итерацию.
		// Причина https://github.com/terratensor/tg-svodd-bot/issues/13
		chunk = strings.TrimSpace(chunk)
		if utf8.RuneCountInString(chunk) == 0 {
			continue
		}

		// Добавляем перенос строки т.к. ранее были обрезаны все пробелы функцией TrimSpace
		builder.WriteString("\n")

		if utf8.RuneCountInString(builder.String())+utf8.RuneCountInString(chunk) < chunkSize {
			builder.WriteString(chunk)
		} else {
			msgs = append(msgs, builder.String())
			builder.Reset()
			builder.WriteString(chunk)
		}

		// Добавляем перенос строки в конце абзаца т.к. ранее были обрезаны все пробелы функцией TrimSpace
		builder.WriteString("\n")
	}
	// При завершении цикла проверяем остался ли в билдере текст,
	// если да, то добавляем текс в срез сообщений
	if builder.Len() > 0 {
		msgs = append(msgs, builder.String())
	}

	return msgs
}

func processBlockquote(node *html.Node) string {
	var text string
	newline := ""
	for el := node.FirstChild; el != nil; el = el.NextSibling {
		if el.Type == html.TextNode {
			// UnescapeString для Data нужен, чтобы избавляться от &quot; в цитатах
			// для последующего корректного чтения в exel, кстати гугл таблицы корректно обрабатывали эти цитаты и не ломали csv
			text += fmt.Sprintf("%v%v", newline, strings.TrimSpace(html.UnescapeString(el.Data)))
			newline = fmt.Sprintf("\n%v", "")
		}
		if el.Type == html.ElementNode && nodeHasRequiredCssClass("author", el) {
			continue
		}
		if el.Type == html.ElementNode && nodeHasRequiredCssClass("link", el) {
			text += fmt.Sprintf("\n%v\n", strings.TrimSpace(getInnerText(el)))
		}
	}

	// return fmt.Sprintf("<i>%v</i>", strings.TrimSpace(html.EscapeString(text)))

	// Текст цитаты разбивается на блоки по разделителю \n и каждый блок оборачивается тегом <i></i>,
	// таким образом, когда в последующем будет производиться проверка на превышение разрешенной длины сообщения 4096,
	// и в случае превышения будет произведена разбивка текста сообщения по разделителю \n,
	// то не должно быть блоков, которые окажутся без закрывающих тегов </i>
	text = strings.TrimSpace(html.EscapeString(text))
	var builder strings.Builder
	chunks := strings.SplitAfter(text, "\n")
	for _, chunk := range chunks {
		builder.WriteString(fmt.Sprintf("<i>%v</i>\n", strings.TrimSpace(chunk)))
	}

	return strings.TrimSpace(builder.String())
}

// Перебирает аттрибуты токена в цикле и возвращает bool
// если в html token найден переданный css class
func nodeHasRequiredCssClass(rcc string, n *html.Node) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			classes := strings.Split(attr.Val, " ")
			for _, class := range classes {
				if class == rcc {
					return true
				}
			}
		}
	}
	return false
}

func getInnerText(node *html.Node) string {
	for el := node.FirstChild; el != nil; el = el.NextSibling {
		if el.Type == html.TextNode {
			return el.Data
		}
	}
	return ""
}
