package flow

import (
	"strconv"
	"unicode/utf8"

	"maxexport/internal/engine"
	"maxexport/internal/faq"
	"maxexport/internal/report"
	"maxexport/internal/service"
	"maxexport/internal/store"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Экран 5. Результат расчёта (ТЗ §15, экран 5; docs/screens.md, «Экран 5»)
// ---------------------------------------------------------------------------

// showResult — отчёт частями («1/2», «2/2»), сразу за ним файл .txt и кнопки результата.
func (b *Bot) showResult(s *Session, id string) []Message {
	c, err := b.svc.Get(id)
	if err != nil {
		return b.calcNotFound(s)
	}
	s.enter(scrResult)
	s.calcID = id

	var out []Message
	for _, part := range c.Report.ChatParts(report.ChatLimit) {
		out = append(out, Message{Text: part})
	}
	// Файл .txt приходит сразу, без отдельного нажатия (решение команды от 22.09.2026).
	out = append(out, Message{
		Text:    b.fileCaption(c),
		File:    &File{Name: c.Report.FileName(), Content: []byte(c.Report.Text())},
		Buttons: b.resultButtons(c),
	})
	return out
}

// showResultCard — короткое напоминание о результате с кнопками (не весь отчёт заново):
// после справки, вопроса, устаревшей кнопки.
func (b *Bot) showResultCard(s *Session, id string) []Message {
	c, err := b.svc.Get(id)
	if err != nil {
		return b.calcNotFound(s)
	}
	s.enter(scrResult)
	s.calcID = id
	r := c.Result
	text := "**" + texts.T("result.title") + "**: " + r.Product.Name + " → " + r.Country.Flag + " " + r.Country.Name +
		", " + engine.FormatDate(r.At)
	return []Message{{Text: text, Buttons: b.resultButtons(c)}}
}

// resultButtons — кнопки результата (ТЗ §15, экран 5). Они привязаны к расчёту, а не к шагу:
// «Скопировать» под старым отчётом скопирует именно его.
func (b *Bot) resultButtons(c *store.Calc) [][]Button {
	rows := [][]Button{
		row(texts.T("btn.copy"), global(gCopy, c.ID)),
		row(texts.T("btn.download"), global(gDl, c.ID)),
		row(texts.T("btn.new_calc"), global(gNew)),
		row(texts.T("btn.edit_data"), global(gEdit, c.ID)),
		row(texts.T("btn.ask"), global(gAsk, c.ID)),
	}
	if c.Result.Stop != nil {
		rows = append(rows, row(texts.T("btn.alt_markets"), global(gAlt, c.ID)))
	}
	return rows
}

func (b *Bot) fileCaption(c *store.Calc) string {
	r := c.Result
	return texts.T("result.download_caption", "product", r.Product.Name, "country", r.Country.Name, "date", engine.FormatDate(r.At))
}

func (b *Bot) calcNotFound(s *Session) []Message {
	s.enter(scrStart)
	return []Message{{Text: texts.T("error.calc_not_found"), Buttons: [][]Button{row(texts.T("btn.new_calc"), global(gNew))}}}
}

// resultAction — кнопки результата и вопросов.
func (b *Bot) resultAction(s *Session, p parsed) []Message {
	id := p.arg(0)
	c, err := b.svc.Get(id)
	if err != nil {
		return b.calcNotFound(s)
	}
	switch p.action {
	case gCopy:
		// Краткая сводка — всегда одно сообщение (ТЗ §15: «единым текстовым сообщением»).
		s.enter(scrResult)
		s.calcID = id
		return []Message{
			{Text: c.Report.Summary(report.CopyLimit)},
			{Text: texts.T("result.copy_hint"), Buttons: b.resultButtons(c)},
		}
	case gDl:
		s.enter(scrResult)
		s.calcID = id
		return []Message{{
			Text:    b.fileCaption(c),
			File:    &File{Name: c.Report.FileName(), Content: []byte(c.Report.Text())},
			Buttons: b.resultButtons(c),
		}}
	case gEdit:
		// «✏️ Изменить данные»: данные расчёта в черновике, выбор поля (ТЗ §15, §22: без потери страны и кода).
		b.loadDraft(s, c.Result)
		s.demo = service.Demo{}
		return b.showEdit(s)
	case gAsk:
		return b.showAsk(s, id)
	case gQ:
		n, _ := strconv.Atoi(p.arg(1))
		if n < 0 || n >= len(faq.Suggestions) {
			return b.showAsk(s, id)
		}
		s.calcID = id
		return b.answer(s, faq.Suggestions[n])
	case gAlt:
		return b.showAlt(s, c)
	case gAltCalc:
		b.loadDraft(s, c.Result)
		s.d.Country = p.arg(1)
		return b.calculate(s)
	case gResult:
		return b.showResultCard(s, id)
	}
	return b.render(s)
}

// ---------- альтернативные рынки (при запрете экспорта) ----------

func (b *Bot) showAlt(s *Session, c *store.Calc) []Message {
	s.enter(scrAlt)
	s.calcID = c.ID
	r := c.Result
	var rows [][]Button
	for i := range b.svc.Countries() {
		other := &b.svc.Countries()[i]
		if other.ID == r.Country.ID {
			continue
		}
		// Проверяем заранее: для этой страны товар тоже запрещён?
		in := r.In
		in.Country = other.ID
		probe := b.svc.Engine.Calculate(in, engine.Env{Now: b.svc.Now(), Rates: b.svc.Rates.Current(b.svc.Now())})
		if probe.Stop == nil {
			rows = append(rows, row(texts.T("btn.calc_for", "flag", other.Flag, "country", other.Name), global(gAltCalc, c.ID, other.ID)))
		}
	}
	rows = append(rows, row(texts.T("btn.back_to_result"), global(gResult, c.ID)))
	text := texts.T("result.alt.intro", "flag", r.Country.Flag, "country", r.Country.Name, "product", r.Product.Name)
	if len(rows) == 1 {
		text = texts.T("result.alt.none")
	}
	return []Message{{Text: text, Buttons: rows}}
}

// ---------- «❓ Задать вопрос» (Экран 6 в режиме вопроса) ----------

func (b *Bot) showAsk(s *Session, id string) []Message {
	c, err := b.svc.Get(id)
	if err != nil {
		return []Message{{Text: texts.T("ask.no_calc"), Buttons: [][]Button{row(texts.T("btn.start_calc"), global(gNew))}}}
	}
	s.enter(scrAsk)
	s.calcID = id
	r := c.Result
	return []Message{{
		Text: lines(texts.T("ask.header", "product", r.Product.Name, "country", r.Country.Name), texts.T("ask.examples")),
		Buttons: [][]Button{
			row(texts.T("btn.back_to_result"), global(gResult, id)),
			row(texts.T("btn.help"), global(gHelp)),
		},
	}}
}

// askText — текст на экране результата или вопроса считается вопросом по расчёту.
func (b *Bot) askText(s *Session, text string) []Message {
	if utf8.RuneCountInString(text) > MaxQuestionLen {
		return []Message{{Text: texts.T("ask.too_long", "limit", engine.FormatInt(MaxQuestionLen))}}
	}
	return b.answer(s, text)
}

// answer — ответ по отчёту и кнопки: первые 3 подсказки и «⬅️ К результату».
func (b *Bot) answer(s *Session, question string) []Message {
	a, err := b.svc.Ask(s.calcID, question)
	if err != nil {
		return []Message{{Text: texts.T("ask.no_calc"), Buttons: [][]Button{row(texts.T("btn.start_calc"), global(gNew))}}}
	}
	s.enter(scrAsk)
	var rows [][]Button
	for i, q := range a.Suggestions {
		if i == 3 {
			break
		}
		rows = append(rows, row(q, global(gQ, s.calcID, strconv.Itoa(i))))
	}
	rows = append(rows, row(texts.T("btn.back_to_result"), global(gResult, s.calcID)))
	return []Message{{Text: a.Text, Buttons: rows}}
}
