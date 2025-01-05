package msgparser

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"tg-svodd-bot/consumer/internal/infra/msgsign"
	"unicode/utf8"

	"golang.org/x/net/html"
)

type Parser struct {
	msgMaxChars   int
	quoteMaxChars int
	quoteMaxWords int
}

func New() *Parser {
	msgMaxChars, err := strconv.Atoi(os.Getenv("MSG_MAX_CHARS"))
	if err != nil || msgMaxChars == 0 {
		msgMaxChars = 4096
	}
	quoteMaxChars, err := strconv.Atoi(os.Getenv("QUOTE_MAX_CHARS"))
	if err != nil || quoteMaxChars == 0 {
		quoteMaxChars = 350
	}
	quoteMaxWords, _ := strconv.Atoi(os.Getenv("QUOTE_MAX_WORDS"))
	if err != nil || quoteMaxWords == 0 {
		quoteMaxWords = 40
	}
	return &Parser{
		msgMaxChars:   msgMaxChars,
		quoteMaxChars: quoteMaxChars,
		quoteMaxWords: quoteMaxWords,
	}
}

// Parse обрабатывает текст сообщения для отправки в телеграм.
// Устанавливает необходимые html теги
func (p *Parser) Parse(msg string, headers map[string]string) ([]string, error) {
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

	messages, err := p.splitMessage(builder.String(), headers)

	return messages, err
}

// splitMessage разбивает текст комментария на блоки размером не более,
// чем 4096 символов с учетом длины подписи
// разделитель для блоков \n
func (p *Parser) splitMessage(msg string, headers map[string]string) ([]string, error) {

	msg = strings.TrimSpace(msg)

	msgsign := msgsign.New(headers)
	limit := p.msgMaxChars - msgsign.Len

	var msgs []string
	if utf8.RuneCountInString(msg) < limit {
		return []string{msg + msgsign.Value}, nil
	}

	var builder strings.Builder
	chunks := strings.SplitAfter(msg, "\n")

	// Сценарий, когда переносов строк не было, но сообщение все еще более 4096 символов
	if len(chunks) == 1 {
		// Если получили ошибку, то возвращаем сообщение с подписью и ловим ошибку от телеги bad request 400
		// TODO подумать, что с этим сделать, верятность ошибки маленькая, почти нулевая
		// Это означает, что в сообщении не было ниодного предложения отделенного точкой.
		// TODO Все таки сделать обработку разделения сообщения по словам(токенам)?
		return splitMessageBySentences(chunks[0], msgsign, limit)
	}

	for _, chunk := range chunks {

		// Удаляем пробелы и если после этого chunk будет пустым то пропускаем итерацию.
		// Причина https://github.com/terratensor/tg-svodd-bot/issues/13
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}

		// Добавляем перенос строки т.к. ранее были обрезаны все пробелы функцией TrimSpace
		// builder.WriteString("\n")

		if utf8.RuneCountInString(builder.String())+(utf8.RuneCountInString(chunk)+2) < limit {
			builder.WriteString(chunk)
			builder.WriteString("\n\n")

		} else {
			msgs = append(msgs, builder.String())
			builder.Reset()
			builder.WriteString(chunk)
			builder.WriteString("\n\n")
		}

		// Добавляем двойной перенос строки в конце абзаца т.к. ранее были обрезаны все пробелы функцией TrimSpace
		// builder.WriteString("\n\n")
	}
	// При завершении цикла проверяем остался ли в билдере текст,
	// если да, то добавляем текст в срез сообщений
	if builder.Len() > 0 {
		builder.WriteString("\n\n")
		msgs = append(msgs, builder.String())
	}

	// Добавляем подпись в последний блок
	return addSignature(msgs, msgsign)
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

	// Cначала удаляем лишние пробелы в начале и конце текста
	text = strings.TrimSpace(text)
	// Ограничиваем размеры цитируемого отрывка
	text = p.truncateText(text)
	// Только после этого запукаем фукцию экранирования специальных символов,
	// т.к. функция после экранирования увеличивает размер строки за счет преобразования символов: characters like "<" to become "&lt;"
	text = html.EscapeString(text)

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
	if len(words) <= p.quoteMaxWords {
		return text
	}
	truncatedText := ""
	for _, word := range words {
		if utf8.RuneCountInString(truncatedText)+utf8.RuneCountInString(word)+1 <= p.quoteMaxChars {
			truncatedText += word + " "
		} else {
			break
		}
	}

	if utf8.RuneCountInString(truncatedText) < count {
		return ModifyString(strings.TrimSpace(truncatedText))
	}
	return truncatedText
}

// ModifyString replaces the last punctuation mark in the input string with an ellipsis.
// It checks the last character of a string and replaces it with "…"
// if it's present in a specified list, otherwise it appends "…" to the end of the string:
func ModifyString(input string) string {
	lastRune, _ := utf8.DecodeLastRuneInString(input)
	punctuationMarks := []rune{' ', '.', ',', ':', ';', '…', '-', '–', '—', '=', '+'}

	for _, punctuationMark := range punctuationMarks {
		if lastRune == punctuationMark {
			modifiedInput := []rune(input)
			modifiedInput[len(modifiedInput)-1] = '…'
			return string(modifiedInput)
		}
	}

	return input + "…"
}

// splitMessageBySentences splits a text chunk into multiple messages based on sentence boundaries.
// Each message is limited to a specified character length, accounting for a signature length.
// It returns a slice of message strings and an error if the generated message is empty.
func splitMessageBySentences(chunk string, msgsign *msgsign.Sign, limit int) ([]string, error) {
	var msgs []string

	// sentences := strings.SplitAfter(chunk, ".")
    re := regexp.MustCompile(`[.?!]\s+`)
    sentences := re.Split(chunk, -1)

	var builder strings.Builder

	for _, sentence := range sentences {

		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		if (utf8.RuneCountInString(builder.String()) + utf8.RuneCountInString(sentence)) < limit {
			builder.WriteString(sentence)
			builder.WriteString(" ")
		} else {
			msgs = append(msgs, strings.TrimSpace(builder.String()))
			builder.Reset()
			builder.WriteString(sentence)
			builder.WriteString(" ")
		}
	}

	// При завершении цикла проверяем остался ли в билдере текст,
	// если да, то добавляем текст в срез сообщений
	if builder.Len() > 0 {
		msgs = append(msgs, builder.String())
	}

	// Добавляем подпись в последний блок
	return addSignature(msgs, msgsign)
}

// addSignature adds the signature to the last message in the given slice of strings.
//
// The signature is added after trimming the last message and the resulting string is
// used to replace the last element in the slice.
//
// The function returns the modified slice of strings.
func addSignature(messages []string, signature *msgsign.Sign) ([]string, error) {
	if len(messages) == 0 {
		return nil, errors.New("func addSignature: empty messages slice")
	}

	lastMessage := messages[len(messages)-1]
	lastMessage = strings.TrimSpace(lastMessage) + signature.Value
	messages[len(messages)-1] = lastMessage
	return messages, nil
}
