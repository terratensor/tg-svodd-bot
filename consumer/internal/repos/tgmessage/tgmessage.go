package tgmessage

import "context"

type TgMessage struct {
	CommentID int
	MessageID int32
}

type TgMessageStoreInterface interface {
	Create(ctx context.Context, tgmessage TgMessage) error
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
