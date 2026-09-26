package flow

import (
	"strconv"
	"time"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/service"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Экран 3. Шаг 2 из 4 — продукт и код ТН ВЭД (docs/screens.md, «Экран 3»)
// ---------------------------------------------------------------------------

// showProduct — «Шаг 2 из 4. Укажите тип продукции».
func (b *Bot) showProduct(s *Session) []Message {
	s.enter(scrProduct)
	s.d.Candidates = nil
	c := b.svc.Engine.Cat.Country(s.d.Country)
	text := lines("**"+texts.T("product.title")+"**", texts.T("product.context", "flag", c.Flag, "country", c.Name), "", texts.T("product.prompt"))
	return []Message{{Text: text, Buttons: [][]Button{
		row(texts.T("btn.manual_code"), s.step(aManual)),
		row(texts.T("btn.back"), s.step(aBack)),
	}}}
}

// showCodeManual — «Введите код ТН ВЭД…».
func (b *Bot) showCodeManual(s *Session) []Message {
	s.enter(scrCodeManual)
	s.d.Candidates = nil
	return []Message{{Text: texts.T("code.prompt"), Buttons: b.codeButtons(s, nil, false)}}
}

func (b *Bot) productAction(s *Session, p parsed) []Message {
	switch p.action {
	case aCode:
		i, err := strconv.Atoi(p.arg(0))
		if err != nil || i < 0 || i >= len(s.d.Candidates) {
			return b.render(s)
		}
		return b.pickCode(s, s.d.Candidates[i], s.screen == scrCodeManual, "")
	case aManual, aRetryCode:
		return b.showCodeManual(s)
	case aRetry: // «Повторить запрос» при недоступном справочнике
		if s.screen == scrCodeManual {
			return b.showCodeManual(s)
		}
		return b.showProduct(s)
	case aSearch:
		return b.showProduct(s)
	case aBack:
		switch {
		case s.screen == scrCodeManual:
			return b.showProduct(s)
		case s.editing && s.d.Code != "":
			return b.showReview(s)
		default:
			return b.showCountry(s)
		}
	}
	return b.render(s)
}

// tnvedUnavailable — демо-режим ведущего: справочник ТН ВЭД недоступен, код проверить
// нечем — предлагаем повторить запрос или ввести код вручную.
func (b *Bot) tnvedUnavailable(s *Session, manual bool) []Message {
	rows := [][]Button{row(texts.T("btn.retry"), s.step(aRetry))}
	if !manual {
		rows = append(rows, row(texts.T("btn.manual_code"), s.step(aManual)))
	}
	rows = append(rows, row(texts.T("btn.back"), s.step(aBack)))
	return []Message{{Text: texts.T("error.tnved_unavailable"), Buttons: rows}}
}

// productText — пользователь написал название товара. Похожий на код текст («1701 12 100 0»)
// обрабатывается как ручной ввод кода.
func (b *Bot) productText(s *Session, text string) []Message {
	if engine.LooksLikeCode(text) {
		return b.codeText(s, text)
	}
	if s.demo.TnvedDown {
		s.enter(scrProduct)
		return b.tnvedUnavailable(s, false)
	}
	s.d.Query = text
	res := b.svc.Search(text)
	input := engine.ClipInput(text, service.MaxInputEcho)
	switch {
	case res.Total == 0:
		s.enter(scrProduct)
		return []Message{{Text: texts.T("product.not_found", "input", input), Buttons: [][]Button{
			row(texts.T("btn.manual_code"), s.step(aManual)),
			row(texts.T("btn.back"), s.step(aBack)),
		}}}
	case res.TooMany:
		s.enter(scrProduct)
		return []Message{{Text: texts.T("product.too_many"), Buttons: [][]Button{
			row(texts.T("btn.manual_code"), s.step(aManual)),
			row(texts.T("btn.back"), s.step(aBack)),
		}}}
	}

	s.enter(scrProduct)
	return b.codeList(s, res.Items, true)
}

// codeList — «Найдено 3 кода ТН ВЭД: 1. … 2. …» и кнопки выбора.
func (b *Bot) codeList(s *Session, items []*data.Product, fromSearch bool) []Message {
	head := texts.T("product.found_one")
	if len(items) > 1 {
		n := int64(len(items))
		head = texts.T("product.found", "n", engine.FormatInt(n),
			"codes_word", engine.Plural(n, texts.T("common.code.one"), texts.T("common.code.few"), texts.T("common.code.many")))
	}
	return []Message{{
		Text:    head + "\n" + b.listCodes(s, items) + "\n\n" + texts.T("product.choose"),
		Buttons: b.codeButtons(s, items, fromSearch),
	}}
}

// redrawCodes — показать список найденных кодов ещё раз (после устаревшей кнопки,
// из справки), чтобы не заставлять пользователя вводить название заново.
func (b *Bot) redrawCodes(s *Session) []Message {
	var items []*data.Product
	for _, code := range s.d.Candidates {
		if p := b.svc.Engine.Cat.Product(code); p != nil {
			items = append(items, p)
		}
	}
	fromSearch := s.screen == scrProduct
	s.enter(s.screen)
	return b.codeList(s, items, fromSearch)
}

// codeText — ручной ввод кода: проверка по справочнику (есть ли такой код, не заменён ли
// он новым, пищевой ли, не введён ли только префикс).
func (b *Bot) codeText(s *Session, text string) []Message {
	if s.demo.TnvedDown {
		manual := s.screen == scrCodeManual
		s.enter(scrCodeManual)
		return b.tnvedUnavailable(s, manual)
	}
	c := b.svc.CheckCode(text)
	code := engine.FormatCode(c.Digits)
	s.enter(scrCodeManual)
	retry := func(msg string) []Message {
		return []Message{{Text: msg, Buttons: [][]Button{
			row(texts.T("btn.retry_code"), s.step(aRetryCode)),
			row(texts.T("btn.search_by_name"), s.step(aSearch)),
			row(texts.T("btn.back"), s.step(aBack)),
		}}}
	}

	switch c.Status {
	case engine.CodeOK:
		return b.pickCode(s, c.Product.Code, true, "")
	case engine.CodeReplaced:
		msg := texts.T("code.replaced", "old", engine.FormatCode(c.Old.Code), "new", engine.FormatCode(c.Product.Code),
			"date", engine.FormatDate(mustDate(c.Old.ReplacedBy.Since)))
		return prepend(msg, b.pickCode(s, c.Product.Code, true, c.Old.Code))
	case engine.CodePrefix:
		if len(c.Candidates) > service.MaxSearchResults {
			return []Message{{Text: texts.T("code.prefix_too_many", "input", code), Buttons: b.codeButtons(s, nil, false)}}
		}
		return []Message{{
			Text:    texts.T("code.prefix", "input", code) + "\n" + b.listCodes(s, c.Candidates),
			Buttons: b.codeButtons(s, c.Candidates, false),
		}}
	case engine.CodeNonFood:
		return retry(texts.T("code.non_food", "code", code, "category", c.Product.Category))
	case engine.CodeNotYetValid:
		return retry(service.NotYetValidMessage(c))
	case engine.CodeNotFound:
		return retry(texts.T("code.not_found", "code", code))
	default:
		return []Message{{Text: texts.T("code.bad_format", "input", engine.ClipInput(text, service.MaxInputEcho)), Buttons: b.codeButtons(s, nil, false)}}
	}
}

// listCodes — «1. 1701 12 100 0 — Сахар-песок свекловичный» и запоминает список для кнопок.
func (b *Bot) listCodes(s *Session, items []*data.Product) string {
	s.d.Candidates = nil
	text := ""
	for i, p := range items {
		s.d.Candidates = append(s.d.Candidates, p.Code)
		if i > 0 {
			text += "\n"
		}
		text += texts.T("product.found_item", "i", strconv.Itoa(i+1), "code", engine.FormatCode(p.Code), "name", p.Name)
	}
	return text
}

// codeButtons — кнопки выбора кода, затем «Указать код вручную» / «Подобрать по названию» и «Назад».
func (b *Bot) codeButtons(s *Session, items []*data.Product, fromSearch bool) [][]Button {
	var rows [][]Button
	for i, p := range items {
		rows = append(rows, row(texts.T("btn.code_choice", "i", strconv.Itoa(i+1), "code", engine.FormatCode(p.Code)), s.step(aCode, strconv.Itoa(i))))
	}
	if fromSearch {
		rows = append(rows, row(texts.T("btn.manual_code"), s.step(aManual)))
	} else {
		rows = append(rows, row(texts.T("btn.search_by_name"), s.step(aSearch)))
	}
	return append(rows, row(texts.T("btn.back"), s.step(aBack)))
}

// pickCode — код выбран: «✅ Код …» (+ пометка «определён автоматически», если код подобран поиском)
// и шаг 3 из 4. В режиме «Изменить» — снова «Проверьте данные» (с проверкой веса единицы).
func (b *Bot) pickCode(s *Session, code string, manual bool, replacedFrom string) []Message {
	p := b.svc.Engine.Cat.Product(code)
	if p == nil {
		return b.showProduct(s)
	}
	// Название из поиска (если было) остаётся в черновике: по нему ядро проверит,
	// подходит ли введённый вручную код к типу продукции (CODE_MISMATCH).
	s.d.Code, s.d.ManualCode, s.d.ReplacedFrom = code, manual, replacedFrom
	msg := texts.T("code.selected", "code", engine.FormatCode(code), "name", p.Name)
	if !manual {
		msg += "\n" + texts.T("code.auto_note")
	}
	if s.editing && s.d.Qty > 0 {
		s.d.UnitWeightConfirmed = false
		return prepend(msg, b.recheckWeight(s))
	}
	return prepend(msg, b.showQty(s))
}

func mustDate(iso string) time.Time {
	t, _ := engine.ParseISODate(iso)
	return t
}
