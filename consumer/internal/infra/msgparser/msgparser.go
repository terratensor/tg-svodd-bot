package msgparser

import (
	"fmt"
	"golang.org/x/net/html"
	"strings"
)

// Parse обрабатывает текст сообщения для отправки в телеграм.
// Устанавливает необходимые html теги
func Parse(msg string) (string, error) {
	n, _ := html.Parse(strings.NewReader(msg))

	var builder strings.Builder
	var f func(*html.Node)

	//text := builder.String()

	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
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

	return builder.String(), nil
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

	return fmt.Sprintf("<i>%v</i>", strings.TrimSpace(text))
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
