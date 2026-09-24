package flow

import (
	"strconv"
	"time"

	"maxexport/internal/engine"
	"maxexport/internal/service"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Режим ведущего (/demo): нестандартные сценарии одной кнопкой (ТЗ §21: «ведущий может
// показать 5 нестандартных сценариев без сбоев»). Все данные учебные.
// ---------------------------------------------------------------------------

// DemoScenario — один сценарий: готовые данные и, если нужно, демо-режим.
type DemoScenario struct {
	Title string // подпись кнопки (до 30 символов)
	About string // что показывает сценарий
	run   func(b *Bot, s *Session) []Message
}

// calcDemo — сценарий, который сразу показывает результат расчёта.
func calcDemo(d draft, demo service.Demo) func(b *Bot, s *Session) []Message {
	return func(b *Bot, s *Session) []Message {
		s.d, s.demo, s.editing = d, demo, false
		s.d.WeightDone = true
		return b.calculate(s)
	}
}

// nextMarch1 — ближайшее 1 марта: в этот день действует квота на вывоз зерна (15.02–30.06).
func nextMarch1(now time.Time) time.Time {
	d := time.Date(now.Year(), time.March, 1, 0, 0, 0, 0, engine.MSK)
	if !d.After(now) {
		d = d.AddDate(1, 0, 0)
	}
	return d
}

// afterReplacement — дата через 2 недели после замены кода (для сценария «код сменится до отгрузки»).
func (b *Bot) afterReplacement(code string, now time.Time) time.Time {
	if p := b.svc.Engine.Cat.Product(code); p != nil && p.ReplacedBy != nil {
		if since, ok := engine.ParseISODate(p.ReplacedBy.Since); ok && since.After(now) {
			return since.AddDate(0, 0, 14)
		}
	}
	return now.AddDate(0, 3, 0)
}

// DemoScenarios — список для /demo. Порядок: 3 сценария ТЗ §22, затем §14.
func (b *Bot) DemoScenarios() []DemoScenario {
	now := b.svc.Now()
	rice := "1006109200"
	if ps := b.svc.Engine.Cat.ProductsWithPrefix("100610"); len(ps) > 0 {
		rice = ps[0].Code
	}
	return []DemoScenario{
		{"1. 🇨🇳 Сахар-песок", "Сценарий 1 ТЗ: полный отчёт по Китаю, пошлина 0 ₽, регистрация GACC",
			calcDemo(draft{Country: "cn", Code: "1701121000", Qty: 5000, UnitWord: "мешков", WeightKg: 250000}, service.Demo{})},
		{"2. 🇦🇲 Мёд натуральный", "Сценарий 2 ТЗ: ЕАЭС, ветеринарный сертификат, реестр ЕАЭС",
			calcDemo(draft{Country: "am", Code: "0409000000", Qty: 200, WeightKg: 4000}, service.Demo{})},
		{"3. 🇰🇿 Мука: вес «5»", "Сценарий 3 ТЗ: нетипичный вес единицы — «кг или тонны?»",
			func(b *Bot, s *Session) []Message {
				s.d, s.demo, s.editing = draft{Country: "kz", Code: "1101000000", Qty: 1000, UnitWord: "мешков"}, service.Demo{}, false
				return b.weightText(s, "5")
			}},
		{"🚫 Запрет вывоза", "Рис-сырец → Китай: временный запрет (учебный пример)",
			calcDemo(draft{Country: "cn", Code: rice, Qty: 20, WeightKg: 20000}, service.Demo{})},
		{"⚠️ Квота на зерно", "Пшеница → Китай с отгрузкой 1 марта: экспортная квота и плавающая пошлина",
			calcDemo(draft{Country: "cn", Code: "1001990000", Qty: 100, WeightKg: 100000, ShipDate: nextMarch1(now)}, service.Demo{})},
		{"⚠️ Запрет ввоза", "Пшеница → Казахстан: страна назначения запретила ввоз",
			calcDemo(draft{Country: "kz", Code: "1001990000", Qty: 100, WeightKg: 100000}, service.Demo{})},
		{"ℹ️ Устаревший код", "Йогурт по коду до 2022 г.: расчёт по новому коду",
			calcDemo(draft{Country: "cn", Code: "0403101100", ManualCode: true, Qty: 500, WeightKg: 6000}, service.Demo{})},
		{"⚠️ Код сменится", "Кофе с отгрузкой после замены кода: код ТН ВЭД заменится до отгрузки",
			calcDemo(draft{Country: "cn", Code: "2101110015", Qty: 200, WeightKg: 2000, ShipDate: b.afterReplacement("2101110015", now)}, service.Demo{})},
		{"⚠️ Сбой курса валют", "Рапс → Китай: курс недоступен, расчёт по последнему известному",
			calcDemo(draft{Country: "cn", Code: "1205109000", Qty: 100, WeightKg: 100000}, service.Demo{RateFail: true})},
		{"⚠️ Скачок курса", "Рапс → Китай: курс евро вырос на 5 % с прошлого расчёта",
			calcDemo(draft{Country: "cn", Code: "1205109000", Qty: 100, WeightKg: 100000}, service.Demo{RateJumpPct: 5})},
		{"⚠️ Сбой справочника", "Справочник ТН ВЭД недоступен: бот не может проверить код (§14.8)",
			func(b *Bot, s *Session) []Message {
				s.d, s.editing = draft{Country: "cn"}, false
				s.demo = service.Demo{TnvedDown: true}
				return b.showProduct(s)
			}},
		{"⚠️ Профиль недоступен", "Сахар → Китай: профиль требований страны недоступен",
			calcDemo(draft{Country: "cn", Code: "1701121000", Qty: 5000, WeightKg: 250000}, service.Demo{ProfileDown: true})},
		{"⚠️ Данные устарели", "Сахар → Китай: могли вступить в силу новые решения",
			calcDemo(draft{Country: "cn", Code: "1701121000", Qty: 5000, WeightKg: 250000}, service.Demo{DataStale: true})},
		{"❄️ Рефрижератор", "Мороженое → Китай: температурный режим −25…−18 °C",
			calcDemo(draft{Country: "cn", Code: "2105009900", Qty: 1000, WeightKg: 8000}, service.Demo{})},
		{"⚠️ Спорный код", "Орехи в меду → Армения: два возможных кода ТН ВЭД",
			calcDemo(draft{Country: "am", Code: "2008199900", Qty: 500, WeightKg: 250}, service.Demo{})},
		{"⚠️ Мак в составе", "Сушки с маком → Китай: компонент под ограничением",
			calcDemo(draft{Country: "cn", Code: "1905909000", Qty: 400, WeightKg: 2000}, service.Demo{})},
		{"⏳ Анализы не успеют", "Сахар → Китай, отгрузка через 3 дня: испытания не успевают",
			calcDemo(draft{Country: "cn", Code: "1701121000", Qty: 5000, WeightKg: 250000, ShipDate: now.AddDate(0, 0, 3)}, service.Demo{})},
	}
}

// showDemo — список сценариев.
func (b *Bot) showDemo(s *Session) []Message {
	if s.screen != scrDemo {
		s.returnTo = s.screen
	}
	s.enter(scrDemo)
	text := lines("**"+texts.T("demo.title")+"**", texts.T("demo.intro"))
	var rows [][]Button
	for i, sc := range b.DemoScenarios() {
		text += "\n" + sc.Title + " — " + sc.About
		rows = append(rows, row(sc.Title, global(gDemoRun, strconv.Itoa(i))))
	}
	rows = append(rows, row(texts.T("btn.back"), s.step(aBack)))
	return []Message{{Text: text, Buttons: rows}}
}

// runDemo запускает сценарий №i.
func (b *Bot) runDemo(s *Session, arg string) []Message {
	list := b.DemoScenarios()
	i, err := strconv.Atoi(arg)
	if err != nil || i < 0 || i >= len(list) {
		return b.showDemo(s)
	}
	return notice("🎓 "+list[i].Title+": "+list[i].About, list[i].run(b, s))
}
