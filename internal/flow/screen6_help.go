package flow

import (
	"maxexport/internal/faq"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Экран 6. Справка (docs/screens.md, «Экран 6»).
// Вопрос по расчёту — в screen5_result.go (showAsk, answer).
// ---------------------------------------------------------------------------

// showHelp — справка одним сообщением; «Назад» вернёт на экран, откуда пришли.
func (b *Bot) showHelp(s *Session) []Message {
	if s.screen != scrHelp {
		s.returnTo = s.screen
	}
	s.enter(scrHelp)
	text := lines("**"+texts.T("help.title")+"**", texts.T("help.intro"))
	for _, a := range faq.Help() {
		text += "\n\n**" + a.Title + "**\n" + a.Text
	}
	return []Message{{Text: text, Buttons: [][]Button{
		row(texts.T("btn.back"), s.step(aBack)),
		row(texts.T("btn.start_calc"), global(gNew)),
		row(texts.T("btn.demo"), global(gDemo)),
	}}}
}

func (b *Bot) helpAction(s *Session, p parsed) []Message {
	if p.action == aBack {
		s.screen = s.returnTo
		return b.render(s)
	}
	return b.render(s)
}
