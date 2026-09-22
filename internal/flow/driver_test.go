package flow

import (
	"strings"
	"testing"
	"time"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/service"
)

// Все тесты считают, что «сегодня» — 21.09.2026, 12:00 по Москве.
var now = time.Date(2026, 9, 21, 12, 0, 0, 0, engine.MSK)

// chat — тестовый собеседник: нажимает кнопки по надписи и пишет текст, как пользователь.
type chat struct {
	t    *testing.T
	bot  *Bot
	user string
	last []Message // ответ бота на последнее действие
	all  []Message // вся переписка
}

func newChat(t *testing.T) *chat {
	t.Helper()
	svc := service.New(data.MustLoad(), engine.TrainingSource{})
	svc.Now = func() time.Time { return now }
	return &chat{t: t, bot: New(svc), user: "tester"}
}

func (c *chat) send(in Input) *chat {
	c.t.Helper()
	c.last = c.bot.Handle(c.user, in)
	c.all = append(c.all, c.last...)
	if len(c.last) == 0 {
		c.t.Fatalf("бот ничего не ответил на %+v", in)
	}
	return c
}

func (c *chat) Start() *chat         { return c.send(Start()) }
func (c *chat) Type(s string) *chat  { return c.send(Text(s)) }
func (c *chat) Press(p string) *chat { return c.send(Action(p)) }
func (c *chat) Unsupported() *chat   { return c.send(Unsupported()) }

// Click нажимает кнопку из последнего ответа, в надписи которой есть label.
func (c *chat) Click(label string) *chat {
	c.t.Helper()
	for i := len(c.last) - 1; i >= 0; i-- {
		for _, r := range c.last[i].Buttons {
			for _, b := range r {
				if strings.Contains(b.Text, label) {
					return c.send(Action(b.Payload))
				}
			}
		}
	}
	c.t.Fatalf("нет кнопки %q. Кнопки: %v\nПоследний ответ:\n%s", label, c.Buttons(), c.Text())
	return c
}

// Buttons — надписи всех кнопок последнего ответа.
func (c *chat) Buttons() []string {
	var out []string
	for _, m := range c.last {
		for _, r := range m.Buttons {
			for _, b := range r {
				out = append(out, b.Text)
			}
		}
	}
	return out
}

// payloadOf — payload кнопки из последнего ответа (для проверки устаревших кнопок).
func (c *chat) payloadOf(label string) string {
	c.t.Helper()
	for _, m := range c.last {
		for _, r := range m.Buttons {
			for _, b := range r {
				if strings.Contains(b.Text, label) {
					return b.Payload
				}
			}
		}
	}
	c.t.Fatalf("нет кнопки %q", label)
	return ""
}

// Text — весь текст последнего ответа.
func (c *chat) Text() string {
	var parts []string
	for _, m := range c.last {
		parts = append(parts, m.Text)
	}
	return strings.Join(parts, "\n---\n")
}

// Expect проверяет, что в последнем ответе есть все фрагменты.
func (c *chat) Expect(fragments ...string) *chat {
	c.t.Helper()
	text := c.Text()
	for _, f := range fragments {
		if !strings.Contains(text, f) {
			c.t.Errorf("в ответе нет %q:\n%s", f, text)
		}
	}
	return c
}

// ExpectNot проверяет, что фрагментов в последнем ответе нет.
func (c *chat) ExpectNot(fragments ...string) *chat {
	c.t.Helper()
	text := c.Text()
	for _, f := range fragments {
		if strings.Contains(text, f) {
			c.t.Errorf("в ответе не должно быть %q:\n%s", f, text)
		}
	}
	return c
}

// ExpectButtons проверяет, что есть кнопки с такими надписями.
func (c *chat) ExpectButtons(labels ...string) *chat {
	c.t.Helper()
	all := strings.Join(c.Buttons(), " | ")
	for _, l := range labels {
		if !strings.Contains(all, l) {
			c.t.Errorf("нет кнопки %q. Кнопки: %s", l, all)
		}
	}
	return c
}

// File — файл из последнего ответа.
func (c *chat) File() *File {
	for _, m := range c.last {
		if m.File != nil {
			return m.File
		}
	}
	return nil
}
