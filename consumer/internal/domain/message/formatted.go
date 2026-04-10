package message

import (
	"unicode/utf8"

	"github.com/gotd/td/tg"
)

// EntityType тип сущности форматирования
type EntityType string

const (
	EntityItalic     EntityType = "italic"
	EntityBold       EntityType = "bold"
	EntityTextURL    EntityType = "text_url"
	EntityBlockquote EntityType = "blockquote"
)

// MessageEntity представляет сущность форматирования в сообщении
type MessageEntity struct {
	Type   EntityType
	Offset int
	Length int
	URL    string // для TextURL
}

// Link представляет ссылку
type Link struct {
	URL  string
	Text string
}

// Signature представляет подпись с источником
type Signature struct {
	Text string
	URL  string
}

// FormattedMessage представляет структурированное сообщение
type FormattedMessage struct {
	Text      string          // Очищенный текст без тегов
	Entities  []MessageEntity // Сущности форматирования
	Quote     string          // Текст цитаты
	Links     []Link          // Ссылки
	Signature *Signature      // Подпись с источником
}

// AddSourceButton добавляет кнопку-источник
func (fm *FormattedMessage) AddSourceButton(url string) {
	fm.Signature = &Signature{
		Text: "★ Источник",
		URL:  url,
	}
}

// FormatSourceLinkHTML возвращает HTML ссылку на источник
func (fm *FormattedMessage) FormatSourceLinkHTML() string {
	if fm.Signature == nil {
		return ""
	}
	return "\n\n<a href=\"" + fm.Signature.URL + "\">" + fm.Signature.Text + "</a>"
}

// FormatSourceLinkMarkdown возвращает Markdown ссылку на источник
func (fm *FormattedMessage) FormatSourceLinkMarkdown() string {
	if fm.Signature == nil {
		return ""
	}
	return "\n\n[" + fm.Signature.Text + "](" + fm.Signature.URL + ")"
}

// AppendSourceLinkHTML добавляет HTML ссылку к тексту
func (fm *FormattedMessage) AppendSourceLinkHTML(text string) string {
	if fm.Signature == nil {
		return text
	}
	return text + fm.FormatSourceLinkHTML()
}

// AppendSourceLinkMarkdown добавляет Markdown ссылку к тексту
func (fm *FormattedMessage) AppendSourceLinkMarkdown(text string) string {
	if fm.Signature == nil {
		return text
	}
	return text + fm.FormatSourceLinkMarkdown()
}

// AddSignatureEntity добавляет подпись к тексту и возвращает обновленный текст + Entity
func (fm *FormattedMessage) AddSignatureEntity(text string, tgEntities *[]tg.MessageEntityClass) string {
	if fm.Signature == nil {
		return text
	}

	sigText := "\n\n" + fm.Signature.Text
	sigOffset := utf8.RuneCountInString(text) + 2

	*tgEntities = append(*tgEntities, &tg.MessageEntityTextURL{
		Offset: sigOffset,
		Length: utf8.RuneCountInString(fm.Signature.Text),
		URL:    fm.Signature.URL,
	})

	return text + sigText
}
