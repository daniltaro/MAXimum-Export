package flow

import (
	"maxexport/internal/engine"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Экран 4. Шаг 3 из 4 — количество и вес (ТЗ §15, экран 4; §22, сценарий 3)
// ---------------------------------------------------------------------------

// summary — «Страна: 🇨🇳 Китай · Продукт: … · Код: …» под заголовком шага (ТЗ §15, экран 4).
func (b *Bot) summary(s *Session) string {
	c := b.svc.Engine.Cat.Country(s.d.Country)
	p := b.svc.Engine.Cat.Product(s.d.Code)
	return texts.T("common.summary", "country", c.Flag+" "+c.Name, "product", p.Name, "code", engine.FormatCode(p.Code))
}

// unitMany — «мешков» для вопроса «Сколько …?»; у насыпных грузов — «тонн (груз навалом)».
func (b *Bot) unitMany(s *Session) string {
	u := b.svc.Engine.Cat.Product(s.d.Code).Unit
	switch u.Many {
	case "":
		return texts.T("qty.unit_default")
	case texts.T("common.tonne.many"):
		return texts.T("qty.unit_bulk")
	}
	return u.Many
}

// unitFor — единица для числа n: слово пользователя или форма из справочника («5 000 мешков»).
func (b *Bot) unitFor(s *Session, n int64) string {
	if s.d.UnitWord != "" {
		return s.d.UnitWord
	}
	u := b.svc.Engine.Cat.Product(s.d.Code).Unit
	if u.Many == "" {
		return texts.T("common.unit.default")
	}
	return engine.Plural(n, u.One, u.Few, u.Many)
}

// ---------- 3а: количество ----------

func (b *Bot) showQty(s *Session) []Message {
	s.enter(scrQty)
	text := lines("**"+texts.T("qty.title")+"**", b.summary(s), "", texts.T("qty.prompt", "unit_many", b.unitMany(s)))
	return []Message{{Text: text, Buttons: [][]Button{row(texts.T("btn.back"), s.step(aBack))}}}
}

func (b *Bot) qtyAction(s *Session, p parsed) []Message {
	if p.action == aBack {
		if s.editing {
			return b.showReview(s)
		}
		return b.showProduct(s)
	}
	return b.render(s)
}

func (b *Bot) qtyText(s *Session, text string) []Message {
	q, err := engine.ParseQuantity(text)
	if err != nil {
		return notice(inputError(err), b.showQty(s))
	}
	s.d.Qty, s.d.UnitWord, s.d.PerUnitKg = q.N, q.UnitWord, q.PerUnitKg
	s.d.UnitWeightConfirmed = false
	accepted := texts.T("qty.accepted", "n", engine.FormatInt(q.N), "unit", b.unitFor(s, q.N))
	if s.editing && s.d.WeightDone {
		return prepend(accepted, b.recheckWeight(s)) // вес уже есть — проверяем вес единицы заново
	}
	return prepend(accepted, b.showWeight(s))
}

// ---------- 3б: вес ----------

func (b *Bot) showWeight(s *Session) []Message {
	s.enter(scrWeight)
	// Заголовок шага и сводка: экран веса часто открывается сам по себе — из «Назад»,
	// «Изменить данные» или после нетипичного веса (ТЗ §15, §22, сценарий 3).
	parts := []string{"**" + texts.T("qty.title") + "**", b.summary(s),
		texts.T("qty.accepted", "n", engine.FormatInt(s.d.Qty), "unit", b.unitFor(s, s.d.Qty)), ""}
	var rows [][]Button
	if s.d.WeightKg > 0 {
		parts = append(parts, texts.T("weight.accepted", "weight", engine.FormatKg(s.d.WeightKg)), "")
	}
	if s.d.PerUnitKg > 0 { // пользователь написал «5000 мешков по 50 кг»
		kg := float64(s.d.Qty) * s.d.PerUnitKg
		args := []string{"n", engine.FormatInt(s.d.Qty), "per_unit", engine.FormatNum(s.d.PerUnitKg), "kg", engine.FormatNum(kg)}
		parts = append(parts, texts.T("weight.suggested", args...), "")
		rows = append(rows, row(texts.T("btn.weight_suggested", args...), s.step(aSuggestW)))
	}
	parts = append(parts, texts.T("weight.prompt"), "", texts.T("weight.skip_hint"))
	rows = append(rows, row(texts.T("btn.skip_weight"), s.step(aSkipW)), row(texts.T("btn.back"), s.step(aBack)))
	return []Message{{Text: lines(parts...), Buttons: rows}}
}

func (b *Bot) weightAction(s *Session, p parsed) []Message {
	switch p.action {
	case aSuggestW:
		return b.acceptWeight(s, float64(s.d.Qty)*s.d.PerUnitKg, 0, true)
	case aSkipW:
		s.d.WeightKg, s.d.NetKg, s.d.WeightDone, s.d.UnitWeightConfirmed = 0, 0, true, false
		return prepend(texts.T("weight.skipped"), b.showReview(s))
	case aKg, aWeightOK: // «5 кг» / «✅ Всё верно»: пользователь подтвердил вес как есть
		s.d.UnitWeightConfirmed = true
		return b.saveWeight(s, s.pendingRawKg, s.pendingNetKg)
	case aTonnes: // «5 тонн (5 000 кг)»
		return b.acceptWeight(s, s.pendingRawKg*1000, s.pendingNetKg*1000, true)
	case aWeightEd: // «✏️ Изменить данные»: страна, код и количество сохраняются (§22)
		return b.showWeight(s)
	case aBack:
		if s.screen == scrWeightCheck {
			return b.showWeight(s)
		}
		if s.editing {
			return b.showReview(s)
		}
		return b.showQty(s)
	}
	return b.render(s)
}

func (b *Bot) weightText(s *Session, text string) []Message {
	w, err := engine.ParseWeight(text)
	if err != nil {
		return notice(inputError(err), b.showWeight(s))
	}
	return b.acceptWeight(s, w.Kg, w.NetKg, w.Explicit)
}

// acceptWeight проверяет вес одной единицы (ТЗ §14.4, §14.9; §22, сценарий 3).
// Типичный вес → шаг 4 из 4. Нетипичный → предупреждение и, если похоже на тонны,
// вопрос «Вы указали 5 — это 5 кг или 5 тонн?».
func (b *Bot) acceptWeight(s *Session, kg, net float64, explicit bool) []Message {
	uc, err := b.svc.CheckWeight(s.d.Code, s.d.Qty, kg, explicit)
	if err != nil {
		return notice(err.Error(), b.showWeight(s))
	}
	s.d.WeightExplicit = explicit
	if !uc.Atypical {
		s.d.UnitWeightConfirmed = false
		return b.saveWeight(s, kg, net)
	}

	s.enter(scrWeightCheck)
	s.pendingRawKg, s.pendingNetKg = kg, net
	p := b.svc.Engine.Cat.Product(s.d.Code)
	text := lines(
		texts.T("weight.atypical", "per_unit", engine.FormatNum(uc.PerUnitKg), "product", p.Name),
		texts.T("weight.atypical_range", "min", engine.FormatNum(uc.Range[0]), "max", engine.FormatNum(uc.Range[1])), "")
	value := engine.FormatNum(kg)
	if uc.TonnesLikely {
		word := tonnesWord(kg)
		text += texts.T("weight.tonnes_question", "value", value, "tonnes_word", word)
		return []Message{{Text: text, Buttons: [][]Button{
			row(texts.T("btn.weight_kg", "value", value), s.step(aKg)),
			row(texts.T("btn.weight_tonnes", "value", value, "tonnes_word", word, "kg", engine.FormatNum(kg*1000)), s.step(aTonnes)),
			row(texts.T("btn.edit_data"), s.step(aWeightEd)),
		}}}
	}
	text += texts.T("weight.confirm_hint")
	return []Message{{Text: text, Buttons: [][]Button{
		row(texts.T("btn.weight_ok"), s.step(aWeightOK)),
		row(texts.T("btn.edit_data"), s.step(aWeightEd)),
	}}}
}

// saveWeight — вес принят: «✅ Вес: …» и шаг 4 из 4.
func (b *Bot) saveWeight(s *Session, kg, net float64) []Message {
	s.d.WeightKg, s.d.NetKg, s.d.WeightDone = kg, net, true
	msg := texts.T("weight.accepted", "weight", engine.FormatKg(kg))
	if net > 0 {
		msg = texts.T("weight.accepted_net", "weight", engine.FormatKg(kg), "net", engine.FormatKg(net))
	}
	return prepend(msg, b.showReview(s))
}

// recheckWeight — после смены кода или количества вес единицы проверяется заново.
func (b *Bot) recheckWeight(s *Session) []Message {
	if s.d.WeightKg <= 0 {
		return b.showReview(s)
	}
	// Вес пользователь сейчас не вводил, поэтому спрашиваем только «всё верно?»,
	// а не «кг или тонны?» — одинаково и после правки на шаге 4, и после «Изменить данные».
	return b.acceptWeight(s, s.d.WeightKg, s.d.NetKg, true)
}

// tonnesWord — «тонна / тонны / тонн» для числа; для дробных — «тонны» (0,5 тонны).
func tonnesWord(v float64) string {
	if v != float64(int64(v)) {
		return texts.T("common.tonne.few")
	}
	return engine.Plural(int64(v), texts.T("common.tonne.one"), texts.T("common.tonne.few"), texts.T("common.tonne.many"))
}
