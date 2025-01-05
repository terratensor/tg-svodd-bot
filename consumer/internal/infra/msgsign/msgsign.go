package msgsign

import (
	"fmt"
	"unicode/utf8"
)

type Sign struct {
	Value string
	Len   int
}

func New(headers map[string]string) *Sign {
	link := headers["comment_link"]
	value := fmt.Sprintf("\n\n★ <a href=\"%v\">Источник</a>", link)

	sign := Sign{Value: value, Len: utf8.RuneCountInString(value)}
	return &sign
}
