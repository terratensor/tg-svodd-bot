package msgparser

import (
	"errors"
	"fmt"
	"log"
	"os"
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

// A ChunkType is the type of a Chunk.
type ChunkType uint32

const (
	ErrorChunk ChunkType = iota
	Text
	Inline
	Blockquote
	LineBreak
)

type Chunk struct {
	Text string
	Type ChunkType
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

	var nodes []Chunk

	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			// В тексте могут содержаться переносы строк, определяем тип строки по наличию перноса
			value := html.EscapeString(n.Data)
			if value != "\n" {
				nodes = append(nodes, Chunk{Text: strings.TrimSpace(value), Type: Text})
			}
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			nodes = append(nodes, Chunk{Text: "\n", Type: LineBreak})
			return
		}
		if n.Type == html.ElementNode && n.Data == "blockquote" {
			nodes = append(nodes, Chunk{Text: p.processBlockquote(n), Type: Blockquote})
			nodes = append(nodes, Chunk{Text: "\n", Type: LineBreak})
			return
		}
		if n.Type == html.ElementNode && nodeHasRequiredCssClass("link", n) {
			link := getInnerText(n)
			link = tgLinkClipper(link)
			nodes = append(nodes, Chunk{Text: link, Type: Inline})
			if containsLineBreak(n) {
				nodes = append(nodes, Chunk{Text: "\n", Type: LineBreak})
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)

	// Форматируем текст, удаляем все повторяющиеся символы новой строки
	formatText(nodes, &builder)

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

	// Сценарии, когда переносов строк не было, но сообщение все еще более 4096 символов
	// Разбиваем по предложениям, по совам, по символам.
	if len(chunks) == 1 {
		// If the message doesn't contain any line breaks, try splitting it into sentences, words, or utf8 characters
		var err error
		var messages []string
		messages, err = splitMessageOnSentences(chunks[0], msgsign, limit)
		if len(messages) == 0 {
			messages, err = splitMessageOnWords(chunks[0], msgsign, limit)
			if len(messages) == 0 {
				log.Println("splitMessageOnWords failed, trying splitTextByUtf8Chars")
				messages, err = splitTextByUtf8Chars(chunks[0], msgsign, limit)
			}
		}
		return messages, err
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
	}
	// При завершении цикла проверяем остался ли в билдере текст,
	// если да, то добавляем текст в срез сообщений
	if builder.Len() > 0 {
		msgs = append(msgs, builder.String())
	}

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

	// Форматирует заданный фрагмент строк цитаты в одну строку, разделенную символами новой строки.
	// Обрезает каждый узел и удаляет все повторяющиеся символы новой строки.
	// Используется для форматирования текста цитаты сообщения перед его отправкой.
	var builder strings.Builder
	flag := 0
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			if flag > 0 {
				continue
			}
			builder.WriteString("\n")
			flag++
			continue
		}
		builder.WriteString(fmt.Sprintf("<i>%v</i>", strings.TrimSpace(chunk)))
		builder.WriteString("\n")
		flag = 0
	}

	return strings.TrimSpace(builder.String())
}

func containsLineBreak(node *html.Node) bool {
	for el := node.FirstChild; el != nil; el = el.NextSibling {
		if el.Data == "br" {
			return true
		}
	}
	return false
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
//
//	func splitMessageOnSentences(chunk string, msgsign *msgsign.Sign, limit int) ([]string, error) {
//		// sentences := strings.SplitAfter(chunk, ".")
//		re := regexp.MustCompile(`[.?!]\s+`)
//		sentences := re.Split(chunk, -1)
//		return splitBlocks(sentences, msgsign, " ", limit)
//	}
func splitMessageOnSentences(chunk string, msgsign *msgsign.Sign, limit int) ([]string, error) {
	punct := map[rune]struct{}{'.': {}, '!': {}, '?': {}, '…': {}}
	words := strings.Fields(chunk)

	var result []string
	var builder strings.Builder

	for _, word := range words {
		lastRune, _ := utf8.DecodeLastRuneInString(word)
		builder.WriteString(word)
		builder.WriteString(" ")
		if _, exists := punct[lastRune]; exists {
			result = append(result, strings.TrimSpace(builder.String()))
			builder.Reset()
		}
	}
	if builder.Len() > 0 && utf8.RuneCountInString(builder.String()) < limit {
		result = append(result, strings.TrimSpace(builder.String()))
	}
	if len(result) > 0 {
		return splitBlocks(result, msgsign, " ", limit)
	}
	return result, nil
}

// splitMessageOnWords splits a text chunk into multiple messages based on word boundaries.
// Each message is limited to a specified character length, accounting for a signature length.
// It returns a slice of message strings and an error if the generated message is empty.
func splitMessageOnWords(chunk string, msgsign *msgsign.Sign, limit int) ([]string, error) {
	words := strings.Fields(chunk)
	// Здесь магическое число 40 слов. Если теста много и пробелов мало,
	// то это возможно и не слова, надо пропустить и делить по символам
	if len(words) > 40 {
		return splitBlocks(words, msgsign, " ", limit)
	}
	return nil, nil
}

func splitTextByUtf8Chars(text string, msgsign *msgsign.Sign, limit int) ([]string, error) {
	var parts []string
	for len(text) > 0 {
		if utf8.RuneCountInString(text) <= limit {
			parts = append(parts, text)
			break
		} else {
			runeCount := 0
			lastSplitIndex := -1
			for i, r := range []rune(text) {
				if runeCount == limit {
					parts = append(parts, string([]rune(text[:i])))
					text = string([]rune(text[i:]))
					break
				} else {
					lastSplitIndex = i + utf8.RuneLen(r)
					runeCount++
				}
			}
			if lastSplitIndex == -1 {
				// this means we have a single character that is longer than limit (4096) utf8 characters, so just add it as is
				parts = append(parts, text)
				break
			} else if len(text) <= lastSplitIndex {
				// This means we've reached the end of the string and didn't find enough runes to make another split
				parts = append(parts, text)
				break
			}
		}
	}
	return addSignature(parts, msgsign)
}

// splitBlocks divides a list of text blocks into smaller message chunks,
// each within a specified character limit, including the length of a separator.
// It ensures that a signature is appended to the last message.
// The function returns a slice of message strings and an error if the process fails.
func splitBlocks(blocks []string, signature *msgsign.Sign, separator string, limit int) ([]string, error) {
	var messages []string
	var builder strings.Builder

	separatorLength := utf8.RuneCountInString(separator)

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		blockLength := utf8.RuneCountInString(block)
		if utf8.RuneCountInString(builder.String())+(blockLength+separatorLength) < limit {
			builder.WriteString(block)
			builder.WriteString(separator)
		} else {
			messages = append(messages, builder.String())
			builder.Reset()
			builder.WriteString(block)
			builder.WriteString(separator)
		}
	}

	if builder.Len() > 0 {
		messages = append(messages, builder.String())
	}

	return addSignature(messages, signature)
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

// formatText форматирует заданный фрагмент строк в одну строку, разделенную символами новой строки.
// Обрезает каждый узел и удаляет все повторяющиеся символы новой строки.
// Используется для форматирования текста сообщения перед его отправкой.
func formatText(nodes []Chunk, builder *strings.Builder) {
	builder.Reset()
	flag := 0
	for n, node := range nodes {
		log.Printf("node: %+v\n", node)
		// Обрабатываем узлы переноса строки
		if node.Type == LineBreak {
			// Если предыдущий узел не Inline, добавляем пробел
			if flag > 1 {
				continue
			}
			builder.WriteString("\n")
			flag++
			continue
		}
		// Обрабатываем узлы текста
		if node.Type == Text {
			if node.Text == "\n" {
				continue
			}
			builder.WriteString(strings.TrimSpace(node.Text))
			// Если следующий узел не Inline ссылка, добавляем перенос строки
			// if len(nodes) > n+1 && nodes[n+1].Type != Inline {
			// 	builder.WriteString("\n")
			// }
			flag = 0
		}
		// Обрабатываем узлы блок-цитаты
		if node.Type == Blockquote {
			builder.WriteString(strings.TrimSpace(node.Text))
			// builder.WriteString("\n")
			flag = 0
		}
		// Обрабатываем узлы ссылки
		if node.Type == Inline {
			// Если предыдущий узел не Inline, добавляем пробел перед ссылкой
			if n-1 > -1 && nodes[n-1].Type != Inline {
				builder.WriteString(" ")
			}
			builder.WriteString(node.Text)
			// Если следующий узел не Inline, добавляем пробел после ссылки
			if len(nodes) > n+1 && nodes[n+1].Type != Inline {
				builder.WriteString(" ")
			}
			flag = 0
		}
	}
}
