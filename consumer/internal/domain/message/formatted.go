package message

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

// HasButton возвращает true если нужно добавить кнопку
func (fm *FormattedMessage) AddSourceButton(url string) {
	fm.Signature = &Signature{
		Text: "★ Источник",
		URL:  url,
	}
}
