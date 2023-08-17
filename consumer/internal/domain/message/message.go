package message

type Message struct {
	ChatID      string      `json:"chat_id"`
	Text        string      `json:"text"`
	ParseMode   string      `json:"parse_mode,omitempty"`
	ReplyMarkup ReplyMarkup `json:"reply_markup,omitempty"`
}

type ReplyMarkup struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard,omitempty"`
}

type InlineButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}
