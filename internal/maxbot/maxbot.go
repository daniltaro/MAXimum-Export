// Package maxbot — адаптер мессенджера MAX: получает события через официальную библиотеку
// github.com/max-messenger/max-bot-api-client-go и передаёт их в диалог (internal/flow),
// а ответы диалога отправляет обратно в MAX.
//
// Здесь нет логики бота — только перевод «событие MAX → flow.Input» и «flow.Message → сообщение MAX».
package maxbot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
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

// DrainTimeout — сколько ждать при остановке, чтобы начатые ответы дошли до пользователей.
const DrainTimeout = 10 * time.Second

// SendRetries — сколько раз повторить отправку при временной ошибке MAX (лимит, 5xx, сеть).
const SendRetries = 3

// Adapter — бот MAX.
type Adapter struct {
	api     *maxapi.Api
	bot     *flow.Bot
	botID   int64  // нужен кнопке «📱 Открыть мини-приложение»
	botName string // @имя бота
	workers sync.WaitGroup

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
	info, err := a.whoAmI(ctx)
	if err != nil {
		return err
	}
	a.botID, a.botName = info.UserID, info.Username
	log.Printf("MAX: бот @%s (%s) запущен, жду сообщений", info.Username, info.FirstName)

	// Воркеры чатов живут в своём контексте: при остановке им дают дописать начатые ответы.
	workCtx, stopWorkers := context.WithCancel(context.Background())
	defer func() {
		done := make(chan struct{})
		go func() { a.workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(DrainTimeout): // не ждём дольше: сообщения досылать некуда
		}
		stopWorkers()
	}()

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
			a.dispatch(workCtx, u)
		}
	}
	return nil
}

// whoAmI проверяет токен. Временную ошибку сети повторяем: из-за неё бот не должен
// выключаться до перезапуска. Останавливаемся только если MAX отверг токен.
func (a *Adapter) whoAmI(ctx context.Context) (model.BotInfo, error) {
	delay := time.Second
	for attempt := 1; ; attempt++ {
		info, err := a.api.Bots.GetMyInfo(ctx)
		if err == nil {
			return info, nil
		}
		var apiErr *maxapi.Error
		if errors.As(err, &apiErr) && (apiErr.Code == "verify.token" || apiErr.Code == "invalid.token") {
			return model.BotInfo{}, fmt.Errorf("MAX не принял токен (%s): проверьте BOT_TOKEN в .env", apiErr.Code)
		}
		if ctx.Err() != nil || attempt >= 10 {
			return model.BotInfo{}, fmt.Errorf("подключение к MAX: %w", cleanErr(err))
		}
		log.Printf("MAX: подключение не удалось (%v), повтор через %v", cleanErr(err), delay)
		sleep(ctx, delay)
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

// dispatch кладёт событие в очередь его чата. У каждого чата свой обработчик, поэтому
// медленная отправка одному пользователю не задерживает других.
func (a *Adapter) dispatch(ctx context.Context, u model.Update) {
	chat := u.ChatID
	if chat == 0 {
		chat = -userOf(u) // событие без чата — ключ по пользователю
	}
	// Кладём событие, не отпуская блокировку: иначе воркер может завершиться по тишине
	// между проверкой и отправкой, и событие пользователя потеряется.
	a.mu.Lock()
	defer a.mu.Unlock()
	q, ok := a.queues[chat]
	if !ok {
		q = make(chan model.Update, 32)
		a.queues[chat] = q
		a.workers.Add(1)
		go a.worker(ctx, chat, q)
	}
	select {
	case q <- u:
	default:
		log.Printf("MAX: очередь чата переполнена, событие пропущено")
	}
}

// worker обрабатывает события одного чата по очереди и завершается после 10 минут тишины.
func (a *Adapter) worker(ctx context.Context, chat int64, q chan model.Update) {
	defer a.workers.Done()
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
				delete(a.lastOut, chat) // память не растёт от разовых собеседников
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
		// Фото, файл и голосовое бот не обрабатывает — даже если у них есть подпись.
		if len(m.Body.Attachments) > 0 || strings.TrimSpace(m.Body.Text) == "" {
			in = flow.Unsupported()
		} else {
			in = flow.Text(strings.TrimSpace(m.Body.Text))
		}
	case model.UpdateMessageCallback:
		cb := u.GetCallback()
		in = flow.Action(cb.Payload)
		// Ответ на нажатие: без него у кнопки крутится индикатор загрузки. Считается
		// в общем лимите обращений к чату, поэтому идёт через ту же паузу.
		a.wait(ctx, u.ChatID)
		if _, err := a.api.Messages.AnswerOnCallback(ctx, cb.CallbackID, model.CallbackAnswer{}); err != nil {
			log.Printf("MAX: ответ на нажатие кнопки: %v", cleanErr(err))
		}
	default:
		return
	}

	user := userOf(u)
	if user == 0 {
		return
	}
	start := time.Now()
	// «Печатает…»: отчёт уходит несколькими сообщениями, и пользователь видит, что бот работает.
	if u.ChatID != 0 {
		_, _ = a.api.Chats.SendAction(ctx, u.ChatID, model.ActionTypingOn)
	}
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

// send отправляет сообщения диалога с паузой SendGap и повторяет временные ошибки:
// потерянная часть отчёта выглядела бы для пользователя как пропавший текст.
func (a *Adapter) send(ctx context.Context, chat, user int64, msgs []flow.Message) {
	for _, m := range msgs {
		out := a.build(ctx, chat, user, m)
		err := a.sendWithRetry(ctx, chat, out)
		if err == nil {
			continue
		}
		if m.File == nil {
			log.Printf("MAX: сообщение не отправлено: %v", cleanErr(err))
			continue
		}
		// Файл отправить не удалось: шлём тот же текст с кнопками, но без вложения,
		// иначе пользователь останется без кнопок результата.
		log.Printf("MAX: файл отчёта не отправлен: %v", cleanErr(err))
		fallback := a.build(ctx, chat, user, flow.Message{
			Text: m.Text + "\n\n" + texts.T("result.download_failed"), Buttons: m.Buttons,
		})
		if err := a.sendWithRetry(ctx, chat, fallback); err != nil {
			log.Printf("MAX: сообщение не отправлено: %v", cleanErr(err))
		}
	}
}

// build собирает сообщение MAX: текст, кнопки и, если есть, файл-вложение.
func (a *Adapter) build(ctx context.Context, chat, user int64, m flow.Message) *maxapi.Message {
	out := maxapi.NewMessage().SetText(toHTML(m.Text)).SetFormat(model.FormatHTML)
	if chat != 0 {
		out.SetChat(chat)
	} else {
		out.SetUser(user)
	}
	if m.File != nil {
		token, err := a.api.Upload.Upload(ctx, model.UploadFile, bytes.NewReader(m.File.Content), m.File.Name, int64(len(m.File.Content)))
		if err != nil {
			log.Printf("MAX: не удалось загрузить файл отчёта: %v", cleanErr(err))
			out.SetText(toHTML(m.Text + "\n\n" + texts.T("result.download_failed")))
		} else {
			out.AddAttachByToken(token, model.AttachFile)
		}
	}
	if kb := a.keyboard(m.Buttons); kb != nil {
		out.AddKeyboard(kb)
	}
	return out
}

// sendWithRetry повторяет отправку при временных ошибках: превышен лимит обращений,
// сбой на стороне MAX, обрыв сети, вложение ещё не готово.
func (a *Adapter) sendWithRetry(ctx context.Context, chat int64, out *maxapi.Message) error {
	delay := time.Second
	var err error
	for attempt := 1; attempt <= SendRetries; attempt++ {
		a.wait(ctx, chat)
		if _, err = a.api.Messages.Send(ctx, out); err == nil || !temporary(err) || ctx.Err() != nil {
			return err
		}
		sleep(ctx, delay)
		delay *= 2
	}
	return err
}

// temporary — стоит ли повторить запрос.
func temporary(err error) bool {
	var apiErr *maxapi.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "attachment.not.ready", "too.many.requests", "service.unavailable", "internal.error":
			return true
		}
		return false
	}
	return true // сетевые ошибки и таймауты
}

// cleanErr убирает из ошибки адрес запроса: в нём бывает подписанная ссылка на загрузку.
func cleanErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
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
				// Мини-приложение привязано к боту в кабинете партнёра MAX; в кнопке
				// передаётся сам бот, а не адрес страницы.
				kr.AddButton(model.Button{Type: model.ButtonOpenApp, Text: b.Text, WebApp: a.botName, ContactID: a.botID})
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
// чтобы «<», «>» и «&» из справочников не ломали форматирование. Кавычки не трогаем:
// в тексте сообщения они допустимы, а числовые замены вида &#34; MAX не описывает.
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func toHTML(s string) string {
	s = htmlEscaper.Replace(s)
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
