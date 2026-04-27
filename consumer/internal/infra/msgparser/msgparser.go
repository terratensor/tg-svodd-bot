package msgparser

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"unicode/utf8"

	"github.com/terratensor/tg-svodd-bot/consumer/internal/domain/message"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/infra/msgsign"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/lib/linkprocessor"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/lib/telegramfilter"
	"github.com/terratensor/tg-svodd-bot/consumer/internal/repos/tgmessage"
	"golang.org/x/net/html"
)

type Parser struct {
	msgMaxChars   int
	quoteMaxChars int
	quoteMaxWords int
	tgmessages    *tgmessage.TgMessages
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
	URL  string // для ссылок и PlainText цитат
}

// ParsedResult результат парсинга с форматированием
type ParsedResult struct {
	HTML          string                      // для обратной совместимости
	Formatted     *message.FormattedMessage   // первый форматированный блок
	Messages      []string                    // разбитое на части (для HTML)
	FormattedMsgs []*message.FormattedMessage // разбитое на части (для MTProto)
}

// BlockquoteResult содержит HTML и чистый текст цитаты
type BlockquoteResult struct {
	HTML      string
	PlainText string
}

func New(tgmessages *tgmessage.TgMessages) *Parser {
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
		tgmessages:    tgmessages,
	}
}

// Parse возвращает результат парсинга с HTML и форматированной версией
// Parse возвращает результат парсинга с HTML и форматированной версией
func (p *Parser) Parse(ctx context.Context, msg string, headers map[string]string) (*ParsedResult, error) {
	n, _ := html.Parse(strings.NewReader(msg))

	var nodes []Chunk

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			value := html.EscapeString(n.Data)
			if value != "\n" {
				value = removeQuotes(value)
				nodes = append(nodes, Chunk{Text: strings.TrimSpace(value), Type: Text})
			}
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			nodes = append(nodes, Chunk{Text: "\n", Type: LineBreak})
			return
		}
		// Добавляем только цитату, без LineBreak обрамления
		if n.Type == html.ElementNode && n.Data == "blockquote" {
			result := p.processBlockquote(ctx, n)
			nodes = append(nodes, Chunk{Text: result.HTML, Type: Blockquote, URL: result.PlainText})
			return
		}
		if n.Type == html.ElementNode && nodeHasRequiredCssClass("link", n) {
			link := getInnerText(n)
			link = linkprocessor.TgLinkClipper(link)
			nodes = append(nodes, Chunk{Text: link, Type: Inline, URL: link})
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

	// Создаем HTML версию и разбиваем на части
	var builder strings.Builder
	formatText(nodes, &builder)
	htmlText := builder.String()

	// Разбиваем HTML на части
	messages, err := p.splitMessage(htmlText, headers)
	if err != nil {
		return nil, err
	}

	// Фильтруем HTML сообщения
	for i, msg := range messages {
		messages[i] = telegramfilter.FilterMessage(msg)
	}

	// Создаем форматированное сообщение для MTProto
	formatted := p.buildFormattedMessage(nodes, headers)

	// Разбиваем форматированное сообщение на части
	formattedMsgs := p.splitFormattedMessage(formatted, headers)

	return &ParsedResult{
		HTML:          htmlText,
		Formatted:     formattedMsgs[0],
		Messages:      messages,
		FormattedMsgs: formattedMsgs,
	}, nil
}

// buildFormattedMessage создает структурированное сообщение из чанков
func (p *Parser) buildFormattedMessage(nodes []Chunk, headers map[string]string) *message.FormattedMessage {
	fm := &message.FormattedMessage{
		Entities: []message.MessageEntity{},
		Links:    []message.Link{},
	}

	var textBuilder strings.Builder
	offset := 0
	flag := 0

	for n, node := range nodes {
		if node.Type == Blockquote {
			// node.URL = PlainText цитаты
			cleanText := strings.TrimSpace(node.URL)
			if cleanText == "" {
				continue
			}
			// Добавляем \n\n перед цитатой (кроме самой первой)
			if textBuilder.Len() > 0 {
				textBuilder.WriteString("\n\n")
				offset += 2
			}
			textBuilder.WriteString(cleanText)
			textBuilder.WriteString("\n")

			fm.Entities = append(fm.Entities, message.MessageEntity{
				Type:   message.EntityBlockquote,
				Offset: offset,
				Length: utf8.RuneCountInString(cleanText),
			})
			offset += utf8.RuneCountInString(cleanText) + 1
			flag = 0
			continue
		}

		if node.Type == LineBreak {
			if flag > 1 {
				continue
			}
			textBuilder.WriteString("\n")
			offset += 1
			flag++
			continue
		}

		if node.Type == Text {
			// Пропускаем текст который является частью LineBreak перед цитатой
			if node.Text == "\n" {
				continue
			}
			cleanText := strings.TrimSpace(html.UnescapeString(node.Text))
			if cleanText == "" {
				continue
			}
			textBuilder.WriteString(cleanText)
			offset += utf8.RuneCountInString(cleanText)
			flag = 0
		}

		if node.Type == Inline {
			cleanText := strings.TrimSpace(html.UnescapeString(node.Text))
			if cleanText == "" {
				continue
			}
			// Добавляем пробел перед ссылкой если предыдущий узел не LineBreak
			if n-1 > -1 && nodes[n-1].Type != LineBreak {
				textBuilder.WriteString(" ")
				offset += 1
			}

			if node.URL != "" {
				fm.Entities = append(fm.Entities, message.MessageEntity{
					Type:   message.EntityTextURL,
					Offset: offset,
					Length: utf8.RuneCountInString(cleanText),
					URL:    node.URL,
				})
			}
			textBuilder.WriteString(cleanText)
			offset += utf8.RuneCountInString(cleanText)

			// Добавляем пробел после ссылки если следующий узел не LineBreak
			if len(nodes) > n+1 && nodes[n+1].Type != LineBreak {
				textBuilder.WriteString(" ")
				offset += 1
			}
			flag = 0
		}
	}

	rawText := strings.TrimSpace(textBuilder.String())
	fm.Text = html.UnescapeString(rawText)

	// Добавляем подпись с источником
	if link, ok := headers["comment_link"]; ok && link != "" {
		// Кодируем только пробелы в fragment, избегая двойного кодирования
		if idx := strings.Index(link, "#:~:text="); idx != -1 {
			prefix := link[:idx]
			fragment := link[idx+1:]
			// Заменяем пробелы на %20, но не трогаем уже закодированные %25
			fragment = strings.ReplaceAll(fragment, " ", "%20")
			link = prefix + "#" + fragment
		}

		fm.Signature = &message.Signature{
			Text: "★ Источник",
			URL:  link,
		}
	}

	return fm
}

// splitFormattedMessage разбивает длинное форматированное сообщение на части
func (p *Parser) splitFormattedMessage(fm *message.FormattedMessage, headers map[string]string) []*message.FormattedMessage {
	if fm == nil {
		return nil
	}

	// Вычисляем лимит с учетом подписи
	signLen := 0
	if fm.Signature != nil {
		signLen = utf8.RuneCountInString("\n\n" + fm.Signature.Text)
	}
	limit := p.msgMaxChars - signLen

	text := fm.Text
	if utf8.RuneCountInString(text) <= limit {
		return []*message.FormattedMessage{fm}
	}

	// Разбиваем текст по \n
	chunks := strings.SplitAfter(text, "\n")
	var result []*message.FormattedMessage
	var currentText strings.Builder
	var currentEntities []message.MessageEntity
	offset := 0

	for _, chunk := range chunks {
		chunkLen := utf8.RuneCountInString(chunk)
		if utf8.RuneCountInString(currentText.String())+chunkLen < limit {
			currentText.WriteString(chunk)
			offset += chunkLen
		} else {
			// Сохраняем текущую часть
			part := &message.FormattedMessage{
				Text:     strings.TrimSpace(currentText.String()),
				Entities: currentEntities,
				Quote:    fm.Quote,
			}
			result = append(result, part)

			// Начинаем новую часть
			currentText.Reset()
			currentText.WriteString(chunk)
			currentEntities = nil
			offset = chunkLen
		}
	}

	// Добавляем последнюю часть
	if currentText.Len() > 0 {
		part := &message.FormattedMessage{
			Text:     strings.TrimSpace(currentText.String()),
			Entities: currentEntities,
			Quote:    fm.Quote,
		}
		// Добавляем подпись только к последней части
		part.Signature = fm.Signature
		result = append(result, part)
	}

	// Если разбивка не удалась, возвращаем как есть
	if len(result) == 0 {
		return []*message.FormattedMessage{fm}
	}

	return result
}

// splitMessage разбивает HTML сообщение на части (без изменений)
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

	if len(chunks) == 1 {
		messages, err := splitMessageOnSentences(chunks[0], msgsign, limit)
		if len(messages) == 0 {
			messages, err = splitMessageOnWords(chunks[0], msgsign, limit)
			if len(messages) == 0 {
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

	if builder.Len() > 0 {
		msgs = append(msgs, builder.String())
	}

	return addSignature(msgs, msgsign)
}

func (p *Parser) processBlockquote(ctx context.Context, node *html.Node) BlockquoteResult {
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
			// log.Printf("username: %v", getInnerText(el))
			err := p.tgmessages.UpdateUsername(ctx, tgmessage.TgMessageUsername{Username: getInnerText(el)})
			if err != nil {
				log.Println(err)
			}
			continue
		}
		if el.Type == html.ElementNode && nodeHasRequiredCssClass("link", el) {
			link := getInnerText(el)
			link = linkprocessor.TgLinkClipper(link)
			text += fmt.Sprintf(" %v ", link)
		}
	}

	// Удаляем цитаты [quote:12345] [/quote]
	text = removeQuotes(text)
	// return fmt.Sprintf("<i>%v</i>", strings.TrimSpace(html.EscapeString(text)))

	// Текст цитаты разбивается на блоки по разделителю \n и каждый блок оборачивается тегом <i></i>,
	// таким образом, когда в последующем будет производиться проверка на превышение разрешенной длины сообщения 4096,
	// и в случае превышения будет произведена разбивка текста сообщения по разделителю \n,
	// то не должно быть блоков, которые окажутся без закрывающих тегов </i>

	// Cначала удаляем лишние пробелы в начале и конце текста
	text = strings.TrimSpace(text)
	// Ограничиваем размеры цитируемого отрывка
	text = p.truncateText(text)
	// Удаляем никнеймы из цитаты
	text = p.removeUsernames(ctx, text)

	// Сохраняем чистый текст для FormattedMessage (без HTML тегов)
	plainText := text

	// Только после этого запукаем фукцию экранирования специальных символов,
	// т.к. функция после экранирования увеличивает размер строки за счет преобразования символов: characters like "<" to become "&lt;"
	text = html.EscapeString(text)

	// Изменена логика, разбиваем цитату по разделителю \n и каждый блок оборачивается тегом <i></i>,
	// таким образом, когда в последующем будет производиться проверка на превышение разрешенной длины сообщения 4096,
	// и в случае превышения будет произведена разбивка текста сообщения по разделителю \n,
	// то не должно быть блоков, которые окажутся без закрывающих тегов </i>

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

	// Сохраняем HTML версию
	htmlText := strings.TrimSpace(builder.String())

	// Сохраняем чистый текст в поле Quote для FormattedMessage
	// (нужно добавить поле в структуру или возвращать два значения)

	return BlockquoteResult{
		HTML:      htmlText,
		PlainText: plainText,
	}
}
func containsLineBreak(node *html.Node) bool {
	for el := node.FirstChild; el != nil; el = el.NextSibling {
		if el.Data == "br" {
			return true
		}
	}
	return false
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
		// log.Printf("node: %+v\n", node)
		// Обрабатываем узлы переноса строки
		if node.Type == LineBreak {
			// Если 2 предыдущих узла не Inline, добавляем перенос строки
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
			// Если предыдущий узел не LineBreak перенос строки, добавляем пробел перед ссылкой
			if n-1 > -1 && nodes[n-1].Type != LineBreak {
				builder.WriteString(" ")
			}
			builder.WriteString(node.Text)
			// Если следующий узел не LineBreak перенос строки, добавляем пробел после ссылки
			if len(nodes) > n+1 && nodes[n+1].Type != LineBreak {
				builder.WriteString(" ")
			}
			flag = 0
		}
	}
}

// removeQuotes удаляет цитаты из текста.
func removeQuotes(text string) string {
	// Регулярное выражение для поиска [quote:] или [quote: с любыми цифрами
	quotePattern := regexp.MustCompile(`\[quote:[0-9]+\]?`)
	// Удаляем все вхождения конструкции [quote:] или [quote:
	text = quotePattern.ReplaceAllString(text, "")
	// Регулярное выражение для поиска [/quote]
	closeQuotePattern := regexp.MustCompile(`\[/quote\]`)
	// Удаляем все вхождения конструкции [/quote]
	text = closeQuotePattern.ReplaceAllString(text, "")
	// Регулярное выражение для поиска /quote] без открывающей скобки
	bareQuotePattern := regexp.MustCompile(`/quote\]`)
	// Удаляем все вхождения конструкции /quote]
	text = bareQuotePattern.ReplaceAllString(text, "")
	return text
}

// removeUsernames удаляет никнеймы из текста.
func (p *Parser) removeUsernames(ctx context.Context, text string) string {
	text, err := p.tgmessages.CutUsernames(ctx, text)
	if err != nil {
		log.Println(err)
	}
	return text
}
