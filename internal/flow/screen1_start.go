package flow

import (
	"time"

	"maxexport/internal/engine"
	"maxexport/internal/service"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Экран 1. Старт (ТЗ §15, экран 1; docs/screens.md, «Экран 1»)
// ---------------------------------------------------------------------------

// showStart — приветствие или «С возвращением!», если в памяти есть прошлый расчёт.
func (b *Bot) showStart(s *Session) []Message {
	s.enter(scrStart)
	s.editing = false

	if last, err := b.svc.Get(s.lastID); err == nil {
		r := last.Result
		return []Message{{
			Text: texts.T("start.returning", "product", r.Product.Name, "country", r.Country.Name, "date", engine.FormatDate(r.At)),
			Buttons: [][]Button{
				row(texts.T("btn.repeat_calc"), global(gRepeat)),
				row(texts.T("btn.new_calc_start"), global(gNew)),
				row(texts.T("btn.help"), global(gHelp)),
			},
		}}
	}

	text := lines(texts.T("start.welcome"), texts.T("start.countries"), "", texts.T("start.hint"), "", texts.T("common.pii_note"))
	buttons := [][]Button{
		row(texts.T("btn.start_calc"), global(gNew)),
		row(texts.T("btn.example"), global(gExample)),
		row(texts.T("btn.help"), global(gHelp)),
	}
	if b.MiniAppURL != "" {
		text += "\n\n" + texts.T("start.miniapp_hint")
		buttons = append(buttons, []Button{{Text: texts.T("btn.open_miniapp"), OpenApp: b.MiniAppURL}})
	}
	return []Message{{Text: text, Buttons: buttons}}
}

// startText — пользователь пишет текст на стартовом экране. Если это страна («Китай»),
// сразу переходим к выбору страны; иначе подсказываем нажать кнопку.
func (b *Bot) startText(s *Session, text string) []Message {
	if m := b.svc.Engine.MatchCountry(text); m.Country != nil {
		s.d = draft{}
		s.enter(scrCountry)
		return b.countryText(s, text)
	}
	return notice(texts.T("error.use_buttons"), b.showStart(s))
}

// newCalc — «Начать расчёт» / «Новый расчёт»: черновик очищается, прошлый расчёт остаётся
// в памяти (для «С возвращением!» и предупреждения о скачке курса).
func (b *Bot) newCalc(s *Session) []Message {
	s.d = draft{}
	s.editing, s.demo = false, service.Demo{}
	return b.showCountry(s)
}

// example — «Пример заполнения»: сразу расчёт по тестовым данным ТЗ §9.
func (b *Bot) example(s *Session) []Message {
	s.d = draft{Country: "cn", Code: "1701121000", Qty: 5000, UnitWord: "мешков", WeightKg: 250000, WeightDone: true}
	s.editing, s.demo = false, service.Demo{}
	msgs := b.calculate(s)
	return notice(texts.T("result.example_badge"), msgs)
}

// repeat — «🔁 Повторить расчёт»: данные прошлого расчёта на шаге 4 из 4.
func (b *Bot) repeat(s *Session) []Message {
	last, err := b.svc.Get(s.lastID)
	if err != nil {
		return notice(texts.T("error.calc_not_found"), b.newCalc(s))
	}
	b.loadDraft(s, last.Result)
	if !s.d.ShipDate.IsZero() && engine.Day(s.d.ShipDate).Before(engine.Day(b.svc.Now())) {
		s.d.ShipDate = time.Time{} // дата отгрузки уже прошла — сбрасываем
	}
	s.editing = false
	return b.showReview(s)
}

// loadDraft заполняет черновик данными расчёта (повтор, «Изменить данные», другие рынки).
func (b *Bot) loadDraft(s *Session, r engine.Result) {
	s.d = draft{
		Country: r.Country.ID, Code: r.Product.Code, Query: r.In.Query, ManualCode: r.In.ManualCode,
		ReplacedFrom: r.In.ReplacedFrom, Qty: r.In.Qty, UnitWord: r.In.UnitWord,
		WeightKg: r.In.WeightKg, NetKg: r.In.NetKg, WeightDone: true, WeightExplicit: true,
		UnitWeightConfirmed: r.In.UnitWeightConfirmed, ShipDate: r.In.ShipDate,
	}
}
