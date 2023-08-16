package msgparser

import (
	"fmt"
	"strings"

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
// ToDo сделать проверку на начала цитаты — теги <li></li>
// ToDo не добавлять цитаты в сообщение если оно уже превышает определенную длин, например 3500,???
// ToDo проверять, что тег цитаты закрыт в итоговом сообщении, можно искать открывающий тег <li>,
// ToDo потом подсчитывать блоки и если следующий блок в сумме с предыдущими превышает 4096, проверять закрыт ли тег <li>
// ToDo если нет, то закрывать, а в следующем чанке, который пойдет в новое сообщение открывать этот тег <li> снова
func splitMessage(msg string, chunkSize int) []string {
	var msgs []string
	if len(msg) < chunkSize {
		msgs = append(msgs, msg)
		return msgs
	}

	var builder strings.Builder
	chunks := strings.SplitAfter(msg, "\n")

	for _, chunk := range chunks {
		if len(builder.String())+len(chunk) < chunkSize {
			builder.WriteString(chunk)
		} else {
			msgs = append(msgs, builder.String())
			builder.Reset()
			builder.WriteString(chunk)
		}
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

	return fmt.Sprintf("<i>%v</i>", strings.TrimSpace(html.EscapeString(text)))
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
