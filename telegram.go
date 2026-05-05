package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Telegram struct {
	bot    *bot.Bot
	chatID string
}

func NewTelegram(_ context.Context, token, chatID string) (*Telegram, error) {
	b, err := bot.New(token)
	if err != nil {
		return nil, err
	}
	return &Telegram{bot: b, chatID: chatID}, nil
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func htmlEscape(s string) string { return htmlEscaper.Replace(s) }

func (t *Telegram) Send(ctx context.Context, id int64, title, url string) error {
	text := fmt.Sprintf(
		`<b>%s</b>`+"\n"+`<a href="%s">Article</a>, <a href="https://news.ycombinator.com/item?id=%d">Comments</a>`,
		htmlEscape(title), htmlEscape(url), id,
	)
	_, err := t.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    t.chatID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
	})
	return err
}
