package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
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

type DBPgMessageUsername struct {
	Username string `db:"username"`
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

// UpdateUsername inserts the given tgmessage.TgMessageUsername into the tg_comments_messages_usernames
// table if it does not already exist.
func (ms *Messages) UpdateUsername(ctx context.Context, username tgmessage.TgMessageUsername) error {
	dbu := &DBPgMessageUsername{
		Username: username.Username,
	}

	_, err := ms.db.ExecContext(ctx, `
    	INSERT INTO tg_comments_messages_usernames (username)
    	VALUES ($1)
	`, dbu.Username)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, err = ms.db.ExecContext(ctx, `
            SELECT $1
        `, dbu.Username)
		}
	}

	return err
}

// AllUsernames retrieves all usernames from the tg_comments_messages_usernames table
// and sends them through a channel. It returns a channel of TgMessageUsername and
// an error. The retrieval and sending process is performed in a separate goroutine.
// The channel is closed once all usernames have been processed or if an error occurs
// during the query or scanning process.
func (ms *Messages) AllUsernames(ctx context.Context, text string) (chan tgmessage.TgMessageUsername, error) {

	chout := make(chan tgmessage.TgMessageUsername, 100)

	go func() {
		defer close(chout)
		dbu := &DBPgMessageUsername{}

		rows, err := ms.db.QueryContext(ctx, `
        	SELECT username
        	FROM tg_comments_messages_usernames
    	`)
		if err != nil {
			err = fmt.Errorf("querying usernames: %w", err)
			log.Println(err)
			return
		}
		defer rows.Close()

		for rows.Next() {
			if err := rows.Scan(&dbu.Username); err != nil {
				err = fmt.Errorf("scanning usernames: %w", err)
				log.Println(err)
				return
			}

			chout <- tgmessage.TgMessageUsername{
				Username: dbu.Username,
			}
		}
	}()

	return chout, nil
}
