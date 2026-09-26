package flow

import (
	"errors"
	"time"

	"maxexport/internal/engine"
	"maxexport/internal/service"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Шаг 4 из 4. «Проверьте данные» (docs/screens.md): сводка перед расчётом — сам ввод
// занимает три шага, а четвёртым идут «Рассчитать», дата отгрузки и «Изменить».
// ---------------------------------------------------------------------------

func (b *Bot) showReview(s *Session) []Message {
	s.enter(scrReview)
	s.editing = false // правка закончена: пользователь снова на сводке
	c := b.svc.Engine.Cat.Country(s.d.Country)
	p := b.svc.Engine.Cat.Product(s.d.Code)

	weight := texts.T("review.row.weight_none")
	switch {
	case s.d.WeightKg > 0 && s.d.NetKg > 0:
		weight = texts.T("review.row.weight_net", "weight", engine.FormatKg(s.d.WeightKg), "net", engine.FormatKg(s.d.NetKg))
	case s.d.WeightKg > 0:
		weight = texts.T("review.row.weight", "weight", engine.FormatKg(s.d.WeightKg))
	}
	date := texts.T("review.row.date_none")
	if !s.d.ShipDate.IsZero() {
		date = texts.T("review.row.date", "date", engine.FormatDate(s.d.ShipDate))
	}

	text := lines(
		"**"+texts.T("review.title")+"**",
		texts.T("review.row.country", "flag", c.Flag, "country", c.Name, "regime", regime(c)),
		texts.T("review.row.product", "product", p.Name),
		texts.T("review.row.code", "code", engine.FormatCode(p.Code)),
		texts.T("review.row.qty", "n", engine.FormatInt(s.d.Qty), "unit", b.unitFor(s, s.d.Qty)),
		weight, date, "", texts.T("review.hint"))
	return []Message{{Text: text, Buttons: [][]Button{
		row(texts.T("btn.calculate"), s.step(aCalc)),
		row(texts.T("btn.ship_date"), s.step(aDate)),
		row(texts.T("btn.edit"), s.step(aEdit)),
		row(texts.T("btn.back"), s.step(aBack)),
	}}}
}

func (b *Bot) reviewAction(s *Session, p parsed) []Message {
	switch p.action {
	case aCalc:
		return b.calculate(s)
	case aDate:
		return b.showDate(s)
	case aClearDate:
		s.d.ShipDate = time.Time{}
		return prepend(texts.T("review.date_removed"), b.showReview(s))
	case aEdit:
		return b.showEdit(s)
	case aEditField:
		return b.editField(s, p.arg(0))
	case aBack:
		if s.screen == scrReview {
			s.editing = false
			return b.showWeight(s)
		}
		return b.showReview(s)
	}
	return b.render(s)
}

// ---------- дата отгрузки (необязательно) ----------

func (b *Bot) showDate(s *Session) []Message {
	s.enter(scrDate)
	var rows [][]Button
	if !s.d.ShipDate.IsZero() {
		rows = append(rows, row(texts.T("btn.clear_date"), s.step(aClearDate)))
	}
	rows = append(rows, row(texts.T("btn.back"), s.step(aBack)))
	return []Message{{Text: texts.T("review.date_prompt"), Buttons: rows}}
}

func (b *Bot) dateText(s *Session, text string) []Message {
	now := b.svc.Now()
	d, ok := engine.ParseDate(text, now)
	switch {
	case !ok:
		return notice(texts.T("review.date_error"), b.showDate(s))
	case engine.Day(d).Before(engine.Day(now)):
		return notice(texts.T("review.date_past", "date", engine.FormatDate(d)), b.showDate(s))
	case d.After(now.AddDate(2, 0, 0)):
		return notice(texts.T("review.date_too_far", "date", engine.FormatDate(d)), b.showDate(s))
	}
	s.d.ShipDate = d
	return prepend(texts.T("review.date_saved", "date", engine.FormatDate(d)), b.showReview(s))
}

// ---------- «✏️ Изменить»: что поменять ----------

func (b *Bot) showEdit(s *Session) []Message {
	s.enter(scrEdit)
	return []Message{{Text: texts.T("review.edit_prompt"), Buttons: [][]Button{
		row(texts.T("btn.edit_country"), s.step(aEditField, "country")),
		row(texts.T("btn.edit_product"), s.step(aEditField, "product")),
		row(texts.T("btn.edit_qty"), s.step(aEditField, "qty")),
		row(texts.T("btn.edit_weight"), s.step(aEditField, "weight")),
		row(texts.T("btn.edit_date"), s.step(aEditField, "date")),
		row(texts.T("btn.back"), s.step(aBack)),
	}}}
}

// editField открывает нужный шаг; после него пользователь вернётся на «Проверьте данные».
func (b *Bot) editField(s *Session, field string) []Message {
	s.editing = true
	switch field {
	case "country":
		return b.showCountry(s)
	case "product":
		return b.showProduct(s)
	case "qty":
		return b.showQty(s)
	case "weight":
		return b.showWeight(s)
	case "date":
		return b.showDate(s)
	}
	return b.showReview(s)
}

// ---------- расчёт ----------

// calculate — «✅ Рассчитать»: сервис проверяет данные, считает, сохраняет расчёт,
// затем показывается Экран 5. Ошибка в данных — сообщение и возврат к нужному шагу.
func (b *Bot) calculate(s *Session) []Message {
	c, err := b.svc.Calculate(service.CalcRequest{
		Country: s.d.Country, Code: s.d.Code, ReplacedFrom: s.d.ReplacedFrom,
		ProductQuery: s.d.Query, ManualCode: s.d.ManualCode,
		Quantity: s.d.Qty, UnitWord: s.d.UnitWord, WeightKg: s.d.WeightKg, NetKg: s.d.NetKg,
		ShipDate: s.d.ShipDate, UnitWeightConfirmed: s.d.UnitWeightConfirmed,
		PreviousID: s.lastID, Demo: s.demo,
	})
	var ie *service.InputError
	switch {
	case errors.As(err, &ie):
		s.editing = true // после исправления — снова «Проверьте данные»
		var next []Message
		switch ie.Field {
		case "country":
			next = b.showCountry(s)
		case "code":
			next = b.showProduct(s)
		case "quantity":
			next = b.showQty(s)
		case "weight_kg", "net_kg":
			next = b.showWeight(s)
		case "ship_date":
			next = b.showDate(s)
		default:
			next = b.showReview(s)
		}
		return notice(ie.Message, next)
	case err != nil:
		return notice(texts.T("error.calc_failed"), b.showReview(s))
	}
	s.editing = false
	s.lastID = c.ID
	return b.showResult(s, c.ID)
}
