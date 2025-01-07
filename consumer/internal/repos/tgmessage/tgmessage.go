package tgmessage

import (
	"context"
	"strings"
)

type TgMessage struct {
	CommentID int
	MessageID int32
}

type TgMessageUsername struct {
	Username string
}

type TgMessageStoreInterface interface {
	Create(ctx context.Context, tgmessage TgMessage) error
	UpdateUsername(ctx context.Context, username TgMessageUsername) error
	AllUsernames(ctx context.Context, text string) (chan TgMessageUsername, error)
}

type TgMessages struct {
	tgmessageStore TgMessageStoreInterface
}

func NewTgMessages(tgmessageStore TgMessageStoreInterface) *TgMessages {
	return &TgMessages{
		tgmessageStore: tgmessageStore,
	}
}

func (tgms *TgMessages) Create(ctx context.Context, tgm TgMessage) error {
	return tgms.tgmessageStore.Create(ctx, tgm)
}

func (tgms *TgMessages) UpdateUsername(ctx context.Context, author TgMessageUsername) error {
	return tgms.tgmessageStore.UpdateUsername(ctx, author)
}

func (tgms *TgMessages) CutUsernames(ctx context.Context, text string) (string, error) {
	chin, err := tgms.tgmessageStore.AllUsernames(ctx, text)
	if err != nil {
		return text, err
	}

	for {
		select {
		case <-ctx.Done():
			return text, nil
		case v, ok := <-chin:
			if !ok {
				return text, nil
			}
			newText, found := tgms.removeSubstring(text, v.Username)
			if !found {
				continue
			} else {
				return newText, nil
			}
		}
	}
}

func (tgms *TgMessages) removeSubstring(text string, substr string) (string, bool) {
	found := false
	for {
		pos := strings.Index(text, substr)
		if pos == -1 {
			break
		}
		text = text[:pos] + text[pos+len(substr):]
		found = true
	}
	return text, found
}
