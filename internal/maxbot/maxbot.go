// Package maxbot — адаптер мессенджера MAX: получает события через официальную библиотеку
// github.com/max-messenger/max-bot-api-client-go и передаёт их в диалог (internal/flow),
// а ответы диалога отправляет обратно в MAX.
//
// Здесь нет логики бота — только перевод «событие MAX → flow.Input» и «flow.Message → сообщение MAX».
package maxbot

import (
	"bytes"
	"context"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	maxapi "github.com/max-messenger/max-bot-api-client-go/v2"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"

	"maxexport/internal/flow"
	"maxexport/texts"
)

// SendGap — пауза между сообщениями в одном чате: MAX принимает не больше 2 сообщений
// в секунду в диалоге, а отчёт — это несколько сообщений подряд.
const SendGap = 550 * time.Millisecond

// Adapter — бот MAX.
type Adapter struct {
	api   *maxapi.Api
	bot   *flow.Bot
	botID int64 // нужен кнопке «📱 Открыть мини-приложение»

	mu      sync.Mutex
	queues  map[int64]chan model.Update // очередь событий каждого чата: ответы не перемешиваются
	lastOut map[int64]time.Time         // когда в чат последний раз ушло сообщение
}

// New подключается к MAX с токеном бота.
func New(token string, bot *flow.Bot) (*Adapter, error) {
	api, err := maxapi.NewApi(token, maxapi.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}))
	if err != nil {
		return nil, err
	}
	return &Adapter{api: api, bot: bot, queues: map[int64]chan model.Update{}, lastOut: map[int64]time.Time{}}, nil
}

// Run проверяет токен, настраивает меню команд и получает события, пока не отменён ctx.
func (a *Adapter) Run(ctx context.Context) error {
	info, err := a.api.Bots.GetMyInfo(ctx)
	if err != nil {
		return err
	}
	a.botID = info.UserID
	log.Printf("MAX: бот @%s (%s) запущен, жду сообщений", info.Username, info.FirstName)

	_, err = a.api.Bots.PatchCommands(ctx, model.BotPatchCommands{Commands: []model.BotCommand{
		{Name: "start", Description: texts.T("common.cmd.start")},
		{Name: "help", Description: texts.T("common.cmd.help")},
		{Name: "demo", Description: texts.T("common.cmd.demo")},
	}})
	if err != nil {
		log.Printf("MAX: не удалось установить меню команд: %v", err)
	}

	var marker int64
	for ctx.Err() == nil {
		updates, next, err := a.api.Subscriptions.GetUpdates(ctx, marker)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("MAX: ошибка получения событий: %v (повтор через 3 с)", err)
			sleep(ctx, 3*time.Second)
			continue
		}
		marker = next
		for _, u := range updates {
			a.dispatch(ctx, u)
		}
	}
	return nil
}

// dispatch кладёт событие в очередь его чата. У каждого чата свой обработчик, поэтому
// медленная отправка одному пользователю не задерживает других.
func (a *Adapter) dispatch(ctx context.Context, u model.Update) {
	chat := u.ChatID
	if chat == 0 {
		chat = -userOf(u) // событие без чата — ключ по пользователю
	}
	a.mu.Lock()
	q, ok := a.queues[chat]
	if !ok {
		q = make(chan model.Update, 32)
		a.queues[chat] = q
		go a.worker(ctx, chat, q)
	}
	a.mu.Unlock()
	select {
	case q <- u:
	default:
		log.Printf("MAX: очередь чата переполнена, событие пропущено")
	}
}

// worker обрабатывает события одного чата по очереди и завершается после 10 минут тишины.
func (a *Adapter) worker(ctx context.Context, chat int64, q chan model.Update) {
	idle := time.NewTimer(10 * time.Minute)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-q:
			a.handle(ctx, u)
			idle.Reset(10 * time.Minute)
		case <-idle.C:
			a.mu.Lock()
			if len(q) == 0 {
				delete(a.queues, chat)
				a.mu.Unlock()
				return
			}
			a.mu.Unlock()
		}
	}
}

// handle переводит событие MAX в событие диалога и отправляет ответ.
func (a *Adapter) handle(ctx context.Context, u model.Update) {
	var in flow.Input
	switch u.UpdateType {
	case model.UpdateBotStarted:
		in = flow.Start()
	case model.UpdateMessageCreated:
		m := u.GetMessage()
		if m.Recipient.ChatType != "" && m.Recipient.ChatType != model.ChatType("dialog") {
			return // бот работает только в личном диалоге, групповые чаты не обслуживает
		}
		switch text := strings.TrimSpace(m.Body.Text); {
		case text != "":
			in = flow.Text(text)
		default:
			in = flow.Unsupported() // фото, файл, голосовое
		}
	case model.UpdateMessageCallback:
		cb := u.GetCallback()
		in = flow.Action(cb.Payload)
		// Ответ на нажатие: без него MAX показывает у кнопки индикатор загрузки.
		note := "✓"
		if _, err := a.api.Messages.AnswerOnCallback(ctx, cb.CallbackID, model.CallbackAnswer{Notification: &note}); err != nil {
			log.Printf("MAX: ответ на нажатие кнопки: %v", err)
		}
	default:
		return
	}

	user := userOf(u)
	if user == 0 {
		return
	}
	start := time.Now()
	msgs := a.bot.Handle(strconv.FormatInt(user, 10), in)
	a.send(ctx, u.ChatID, user, msgs)
	// В журнал — только тип события и время, без текста пользователя (ТЗ §17).
	log.Printf("MAX: %s → %d сообщ. за %v", u.UpdateType, len(msgs), time.Since(start).Round(time.Millisecond))
}

// userOf — кто написал или нажал кнопку.
func userOf(u model.Update) int64 {
	switch u.UpdateType {
	case model.UpdateMessageCallback:
		return u.GetCallback().User.UserID
	case model.UpdateMessageCreated:
		return u.GetMessage().Sender.UserID
	default:
		return u.GetUser().UserID
	}
}

// send отправляет сообщения диалога с паузой SendGap.
func (a *Adapter) send(ctx context.Context, chat, user int64, msgs []flow.Message) {
	for _, m := range msgs {
		a.wait(ctx, chat)
		out := maxapi.NewMessage().SetText(toHTML(m.Text)).SetFormat(model.FormatHTML)
		if chat != 0 {
			out.SetChat(chat)
		} else {
			out.SetUser(user)
		}
		if m.File != nil {
			token, err := a.api.Upload.Upload(ctx, model.UploadFile, bytes.NewReader(m.File.Content), m.File.Name, int64(len(m.File.Content)))
			if err != nil {
				log.Printf("MAX: не удалось загрузить файл отчёта: %v", err)
				out.SetText(toHTML(m.Text + "\n\n" + texts.T("result.download_failed")))
			} else {
				out.AddAttachByToken(token, model.AttachFile)
			}
		}
		if kb := a.keyboard(m.Buttons); kb != nil {
			out.AddKeyboard(kb)
		}
		if _, err := a.api.Messages.Send(ctx, out); err != nil {
			log.Printf("MAX: не удалось отправить сообщение: %v", err)
		}
	}
}

// keyboard — кнопки диалога в формате MAX: callback, а для мини-приложения — open_app.
func (a *Adapter) keyboard(rows [][]flow.Button) *model.Keyboard {
	if len(rows) == 0 {
		return nil
	}
	kb := model.NewKeyboard()
	for _, r := range rows {
		kr := kb.AddRow()
		for _, b := range r {
			if b.OpenApp != "" && a.botID != 0 {
				kr.AddOpenApp(b.Text, a.botID)
			} else {
				kr.AddCallBack(b.Text, b.Payload)
			}
		}
	}
	return kb
}

// wait выдерживает паузу между сообщениями в чате.
func (a *Adapter) wait(ctx context.Context, chat int64) {
	a.mu.Lock()
	next := a.lastOut[chat].Add(SendGap)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	a.lastOut[chat] = next
	a.mu.Unlock()
	sleep(ctx, time.Until(next))
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// toHTML превращает разметку диалога (**жирный**) в HTML для MAX и экранирует спецсимволы,
// чтобы «<», «>» и «&» из справочников не ломали форматирование.
func toHTML(s string) string {
	s = html.EscapeString(s)
	var b strings.Builder
	open := false
	for {
		i := strings.Index(s, "**")
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		if open {
			b.WriteString("</b>")
		} else {
			b.WriteString("<b>")
		}
		open = !open
		s = s[i+2:]
	}
	if open {
		b.WriteString("</b>")
	}
	return b.String()
}
