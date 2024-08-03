package msgparser

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

type Parser struct {
	maxChars int
	maxWords int
}

func New() *Parser {
	maxChars, err := strconv.Atoi(os.Getenv("QUOTE_MAX_CHARS"))
	if err != nil || maxChars == 0 {
		maxChars = 350
	}
	maxWords, _ := strconv.Atoi(os.Getenv("QUOTE_MAX_WORDS"))
	if err != nil || maxWords == 0 {
		maxWords = 40
	}
	return &Parser{
		maxChars: maxChars,
		maxWords: maxWords,
	}
}

// Parse обрабатывает текст сообщения для отправки в телеграм.
// Устанавливает необходимые html теги
func (p *Parser) Parse(msg string) ([]string, error) {
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
			builder.WriteString(fmt.Sprintf("\n%s\n", p.processBlockquote(n)))
			return
		}
		if n.Type == html.ElementNode && nodeHasRequiredCssClass("link", n) {
			link := getInnerText(n)
			link = tgLinkClipper(link)
			builder.WriteString(fmt.Sprintf("%v", link))
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

func (p *Parser) processBlockquote(node *html.Node) string {
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
			link := getInnerText(el)
			link = tgLinkClipper(link)
			text += fmt.Sprintf(" %v", link)
		}
	}

	// return fmt.Sprintf("<i>%v</i>", strings.TrimSpace(html.EscapeString(text)))

	// Текст цитаты разбивается на блоки по разделителю \n и каждый блок оборачивается тегом <i></i>,
	// таким образом, когда в последующем будет производиться проверка на превышение разрешенной длины сообщения 4096,
	// и в случае превышения будет произведена разбивка текста сообщения по разделителю \n,
	// то не должно быть блоков, которые окажутся без закрывающих тегов </i>
	text = strings.TrimSpace(html.EscapeString(text))
	text = p.truncateText(text)

	// Изменена логика, разбиваем цитату по разделителю \n и работаем только с первым элементом среза,
	// обрабатываем этот фрагмент функцией TruncateText и добавляем его в билдер
	chunks := strings.SplitAfter(text, "\n")

	var builder strings.Builder
	for _, chunk := range chunks {
		builder.WriteString(fmt.Sprintf("<i>%v</i>\n", strings.TrimSpace(chunk)))
	}

	return strings.TrimSpace(builder.String())
}

// tgLinkClipper вырезает из url на телеграм канал схему и подставляет нижнее подчеркивание перед адресом
// необходимо для того, чтобы телеграм ссылки отображались как текст и не открывались.
// Исключение составляет канал svoddru
func tgLinkClipper(link string) string {

	if strings.Contains(link, "https://t.me/svoddru") {
		return link
	}

	if strings.Contains(link, "https://t.me") {
		link = strings.ReplaceAll(link, "https://", "")
		link = fmt.Sprintf("_%v", link)
	}

	if strings.Contains(link, "http://t.me") {
		link = strings.ReplaceAll(link, "http://", "")
		link = fmt.Sprintf("_%v", link)
	}

	return link
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

// TruncateText truncates the input text to a certain number of characters or words.
//
// It takes a string input text and truncates it based on the maximum characters and words allowed.
// Returns the truncated text.
func (p *Parser) truncateText(text string) string {
	count := utf8.RuneCountInString(text)
	words := strings.Split(text, " ")
	if len(words) <= p.maxWords {
		return text
	}
	truncatedText := ""
	// TODO улучшить формулу расчета, т.к. символы "", ) и ( и др, установить какие еще.
	// в рунах эти символы занимают 3 позиции, надо определять эти символы и пересчитывать count
	for _, word := range words {
		if utf8.RuneCountInString(truncatedText)+utf8.RuneCountInString(word)+1 <= p.maxChars+10 {
			truncatedText += word + " "
		} else {
			break
		}
	}

	if utf8.RuneCountInString(truncatedText) < count {
		return truncatedText + "…"
	}
	return truncatedText
}
