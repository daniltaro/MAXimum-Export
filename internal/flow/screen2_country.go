package flow

import (
	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/service"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Экран 2. Шаг 1 из 4 — страна (ТЗ §15, экран 2; docs/screens.md, «Экран 2»)
// ---------------------------------------------------------------------------

// showCountry — «Шаг 1 из 4. Выберите страну экспорта» и три кнопки стран.
// Пояснения к странам — в тексте сообщения: подписи на кнопках MAX обрезает.
func (b *Bot) showCountry(s *Session) []Message {
	s.enter(scrCountry)
	text := "**" + texts.T("country.title") + "**\n"
	for i := range b.svc.Countries() {
		c := &b.svc.Countries()[i]
		text += "\n" + texts.T("country.line", "flag", c.Flag, "country", c.Name, "subtitle", c.Subtitle)
	}
	text += "\n\n" + texts.T("country.text_hint")
	return []Message{{Text: text, Buttons: b.countryButtons(s)}}
}

func (b *Bot) countryButtons(s *Session) [][]Button {
	var rows [][]Button
	for i := range b.svc.Countries() {
		c := &b.svc.Countries()[i]
		rows = append(rows, row(texts.T("btn.country", "flag", c.Flag, "country", c.Name), s.step(aCountry, c.ID)))
	}
	return append(rows, row(texts.T("btn.back"), s.step(aBack)))
}

func (b *Bot) countryAction(s *Session, p parsed) []Message {
	switch p.action {
	case aCountry:
		return b.setCountry(s, p.arg(0))
	case aYes:
		return b.setCountry(s, s.pendingCountry)
	case aNo:
		return b.showCountry(s)
	case aBack:
		if s.editing {
			return b.showReview(s)
		}
		return b.showStart(s)
	}
	return b.render(s)
}

// countryText — страна написана текстом: точное название, синоним («КНР») или не поддерживается.
func (b *Bot) countryText(s *Session, text string) []Message {
	m := b.svc.Engine.MatchCountry(text)
	switch {
	case m.Country == nil:
		s.enter(scrCountry)
		msg := texts.T("country.unsupported", "input", engine.ClipInput(text, service.MaxInputEcho))
		return []Message{{Text: msg, Buttons: b.countryButtons(s)}}
	case m.Exact:
		return b.setCountry(s, m.Country.ID)
	default:
		s.pendingInput = engine.ClipInput(text, service.MaxInputEcho)
		return b.confirmCountry(s, m.Country.ID)
	}
}

// confirmCountry — «Вы указали "КНР". Это Китай? Подтвердите.» (ТЗ §15, экран 2).
func (b *Bot) confirmCountry(s *Session, id string) []Message {
	c := b.svc.Engine.Cat.Country(id)
	if c == nil {
		return b.showCountry(s)
	}
	s.enter(scrCountryConfirm)
	s.pendingCountry = c.ID
	return []Message{{
		Text: texts.T("country.confirm", "input", s.pendingInput, "country", c.Name),
		Buttons: [][]Button{
			row(texts.T("btn.country_yes", "country", c.Name), s.step(aYes)),
			row(texts.T("btn.country_no"), s.step(aNo)),
		},
	}}
}

// setCountry — страна выбрана: «✅ Страна: 🇨🇳 Китай (Третьи страны)» и следующий шаг.
// Если пришли из «✏️ Изменить», код сохраняется и возвращаемся на «Проверьте данные».
func (b *Bot) setCountry(s *Session, id string) []Message {
	c := b.svc.Engine.Cat.Country(id)
	if c == nil {
		return b.showCountry(s)
	}
	s.d.Country = c.ID
	selected := texts.T("country.selected", "flag", c.Flag, "country", c.Name, "regime", regime(c)) + "\n" + regimeHint(c)
	if s.editing && s.d.Code != "" {
		return prepend(selected, b.showReview(s))
	}
	return prepend(selected, b.showProduct(s))
}

func regime(c *data.Country) string {
	if c.EAEU {
		return texts.T("country.regime.eaeu")
	}
	return texts.T("country.regime.third")
}

func regimeHint(c *data.Country) string {
	if c.EAEU {
		return texts.T("country.regime_hint.eaeu")
	}
	return texts.T("country.regime_hint.third")
}
