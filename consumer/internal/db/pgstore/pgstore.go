package pgstore

import (
	"context"
	"database/sql"
	"tg-svodd-bot/consumer/internal/repos/tgmessage"

	_ "github.com/jackc/pgx/v4/stdlib" // Postgresql driver

	"time"
)

var _ tgmessage.TgMessageStoreInterface = &Messages{}

type Messages struct {
	db *sql.DB
}

type DBPgMessage struct {
	CommentID int       `db:"comment_id"`
	MessageID int32     `db:"message_id"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

func NewMessages(dsn string) (*Messages, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	err = db.Ping()
	if err != nil {
		db.Close()
		return nil, err
	}
	ls := &Messages{
		db: db,
	}
	return ls, nil
}

func (ms *Messages) Close() {
	ms.db.Close()
}

func (ms *Messages) Create(ctx context.Context, tgm tgmessage.TgMessage) error {
	dbm := &DBPgMessage{
		CommentID: tgm.CommentID,
		MessageID: tgm.MessageID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := ms.db.ExecContext(ctx, `INSERT INTO tg_comments_messages
    (comment_id, message_id, created_at, updated_at)
    values ($1, $2, $3, $4)`,
		dbm.CommentID,
		dbm.MessageID,
		dbm.CreatedAt,
		dbm.UpdatedAt,
	)
	if err != nil {
		return err
	}

	return nil
}
