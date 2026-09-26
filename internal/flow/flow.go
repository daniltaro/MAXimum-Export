package flow

import (
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"maxexport/internal/engine"
	"maxexport/internal/service"
	"maxexport/texts"
)

// Лимиты ввода (docs/screens.md, «Общие правила»).
const (
	MaxTextLen     = 200 // обычный ввод на шагах
	MaxQuestionLen = 500 // вопрос по расчёту
)

// Bot — диалог чат-бота. Один на всю программу; безопасен для одновременной работы
// с разными пользователями (сообщения одного пользователя обрабатываются по очереди).
type Bot struct {
	svc        *service.Service
	sessions   *sessions
	MiniAppURL string // адрес мини-приложения; пусто — кнопку «📱 Открыть» не показываем
}

// New создаёт бота. Сессии живут 24 часа, одновременно — не больше 10 000.
func New(svc *service.Service) *Bot {
	return &Bot{svc: svc, sessions: newSessions(24*time.Hour, 10000)}
}

// Handle обрабатывает событие пользователя user и возвращает сообщения для отправки.
// Никогда не паникует наружу: при внутренней ошибке пользователь увидит error.generic.
func (b *Bot) Handle(user string, in Input) (out []Message) {
	s := b.sessions.get(user, b.svc.Now())
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flow: паника: %v", r) // без текста пользователя — там могут быть его данные
			out = []Message{{Text: texts.T("error.generic"), Buttons: [][]Button{row(texts.T("btn.start_calc"), global(gNew))}}}
		}
	}()
	return b.route(s, in)
}

// route — общие правила, затем обработчик текущего экрана.
func (b *Bot) route(s *Session, in Input) []Message {
	switch in.Kind {
	case InputStart:
		return b.showStart(s)
	case InputUnsupported:
		return notice(texts.T("error.unsupported_message"), b.render(s))
	case InputAction:
		return b.routeAction(s, in.Payload)
	}

	text := strings.TrimSpace(in.Text)
	if strings.HasPrefix(text, "/") {
		return b.command(s, text)
	}
	// На экране результата, в вопросах и на «Альтернативных рынках» текст — это вопрос
	// по расчёту: у него свой лимит и свои сообщения об ошибке, а экран не сбрасывается.
	if s.screen == scrResult || s.screen == scrAsk || s.screen == scrAlt {
		switch {
		case utf8.RuneCountInString(text) > MaxQuestionLen:
			return []Message{{Text: texts.T("ask.too_long", "limit", engine.FormatInt(MaxQuestionLen))}}
		case engine.DetectPII(text) != "":
			return []Message{{Text: texts.T("error.pii", "kind", engine.DetectPII(text))}}
		case text == "":
			return b.render(s)
		}
		return b.askText(s, text)
	}
	if utf8.RuneCountInString(text) > MaxTextLen {
		return notice(texts.T("error.too_long", "limit", engine.FormatInt(MaxTextLen)), b.render(s))
	}
	// Персональные данные бот не принимает и не сохраняет: паспорт, ИНН, телефон
	// и т. п. отсекаются до обработки ввода.
	if kind := engine.DetectPII(text); kind != "" {
		return notice(texts.T("error.pii", "kind", kind), b.render(s))
	}
	if text == "" {
		return b.render(s)
	}

	switch s.screen {
	case scrStart:
		return b.startText(s, text)
	case scrCountry, scrCountryConfirm:
		return b.countryText(s, text)
	case scrProduct:
		return b.productText(s, text)
	case scrCodeManual:
		return b.codeText(s, text)
	case scrQty:
		return b.qtyText(s, text)
	case scrWeight, scrWeightCheck:
		return b.weightText(s, text)
	case scrDate:
		return b.dateText(s, text)
	default: // шаг 4 из 4, выбор поля, справка, режим ведущего — ждём кнопку
		return notice(texts.T("error.use_buttons"), b.render(s))
	}
}

// command — /start, /help, /demo.
func (b *Bot) command(s *Session, text string) []Message {
	cmd := strings.ToLower(strings.Fields(text)[0])
	switch cmd {
	case "/start":
		return b.showStart(s)
	case "/help":
		return b.showHelp(s)
	case "/demo":
		return b.showDemo(s)
	}
	return notice(texts.T("error.unknown_command"), b.render(s))
}

// routeAction — нажатие кнопки.
func (b *Bot) routeAction(s *Session, payload string) []Message {
	p, ok := parsePayload(payload)
	if !ok {
		return notice(texts.T("error.stale_button"), b.render(s))
	}
	if p.global {
		return b.globalAction(s, p)
	}
	if p.view != s.view {
		return notice(texts.T("error.stale_button"), b.render(s))
	}
	switch s.screen {
	case scrStart:
		return b.render(s)
	case scrCountry, scrCountryConfirm:
		return b.countryAction(s, p)
	case scrProduct, scrCodeManual:
		return b.productAction(s, p)
	case scrQty:
		return b.qtyAction(s, p)
	case scrWeight, scrWeightCheck:
		return b.weightAction(s, p)
	case scrReview, scrDate, scrEdit:
		return b.reviewAction(s, p)
	case scrHelp, scrDemo:
		return b.helpAction(s, p)
	default:
		return b.render(s)
	}
}

// globalAction — кнопки, которые работают из любого места.
func (b *Bot) globalAction(s *Session, p parsed) []Message {
	switch p.action {
	case gNew:
		return b.newCalc(s)
	case gStart:
		return b.showStart(s)
	case gHelp:
		return b.showHelp(s)
	case gExample:
		return b.example(s)
	case gRepeat:
		return b.repeat(s)
	case gDemo:
		return b.showDemo(s)
	case gDemoRun:
		return b.runDemo(s, p.arg(0))
	case gCopy, gDl, gEdit, gAsk, gAlt, gAltCalc, gResult, gQ:
		return b.resultAction(s, p)
	}
	return notice(texts.T("error.stale_button"), b.render(s))
}

// render — показать текущий экран заново (после ошибки, устаревшей кнопки, из справки).
func (b *Bot) render(s *Session) []Message {
	switch s.screen {
	case scrCountry:
		return b.showCountry(s)
	case scrProduct, scrCodeManual:
		if len(s.d.Candidates) > 0 {
			return b.redrawCodes(s) // список кодов уже был показан — показываем его снова
		}
		if s.screen == scrCodeManual {
			return b.showCodeManual(s)
		}
		return b.showProduct(s)
	case scrQty:
		return b.showQty(s)
	case scrWeight:
		return b.showWeight(s)
	case scrCountryConfirm:
		return b.confirmCountry(s, s.pendingCountry)
	case scrWeightCheck:
		return b.acceptWeight(s, s.pendingRawKg, s.pendingNetKg, true)
	case scrReview:
		return b.showReview(s)
	case scrDate:
		return b.showDate(s)
	case scrEdit:
		return b.showEdit(s)
	case scrAsk:
		return b.showAsk(s, s.calcID)
	case scrAlt:
		if c, err := b.svc.Get(s.calcID); err == nil {
			return b.showAlt(s, c)
		}
		return b.showResultCard(s, s.calcID)
	case scrResult:
		return b.showResultCard(s, s.calcID)
	case scrHelp:
		return b.showHelp(s)
	case scrDemo:
		return b.showDemo(s)
	default:
		return b.showStart(s)
	}
}

// enter — переход на экран: новый номер показа делает кнопки прошлых сообщений устаревшими.
func (s *Session) enter(scr screen) {
	s.screen = scr
	s.view++
}

// notice — добавить сообщение-пояснение перед сообщениями экрана.
func notice(text string, msgs []Message) []Message {
	return append([]Message{{Text: text}}, msgs...)
}

// prepend — добавить строку в начало первого сообщения (например, «✅ Страна: …»).
func prepend(text string, msgs []Message) []Message {
	if len(msgs) == 0 {
		return []Message{{Text: text}}
	}
	msgs[0].Text = text + "\n\n" + msgs[0].Text
	return msgs
}

// lines склеивает строки сообщения, пропуская пустые элементы-заглушки "".
func lines(parts ...string) string {
	return strings.Join(parts, "\n")
}

// inputError — текст ошибки ввода для пользователя (тексты ошибок ядра — из texts/ru.json).
func inputError(err error) string {
	switch {
	case errors.Is(err, engine.ErrNoNumber):
		return texts.T("error.no_number")
	case errors.Is(err, engine.ErrQtyNonPos):
		return texts.T("qty.error.non_positive")
	case errors.Is(err, engine.ErrQtyNotInt):
		return texts.T("qty.error.not_int")
	case errors.Is(err, engine.ErrWeightNonPos):
		return texts.T("weight.error.non_positive")
	case errors.Is(err, engine.ErrNetGtGross):
		return texts.T("weight.error.net_gt_gross")
	}
	return err.Error()
}
