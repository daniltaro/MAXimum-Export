// Package faq — справка (ТЗ §15, экран 6) и ответы на вопросы по результату расчёта
// (кнопка «❓ Задать вопрос»).
//
// Ответы собираются без нейросети: по ключевым словам вопроса выбирается тема
// («сертификаты», «пошлина», «регистрация»...), и в ответ попадает соответствующая часть
// отчёта. Внешних интеграций по ТЗ §19 в MVP нет.
package faq

import (
	"fmt"
	"strings"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/report"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Справка (ТЗ §15, экран 6)
// ---------------------------------------------------------------------------

// Article — статья справки.
type Article struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// ArticleIDs — статьи справки в порядке показа (тексты — texts/ru.json, help.article.<id>.*).
var ArticleIDs = []string{"tnved", "find_code", "incoterms", "countries", "zero_duty", "code_not_found"}

// Help возвращает все статьи справки.
func Help() []Article {
	var out []Article
	for _, id := range ArticleIDs {
		out = append(out, Article{
			ID:    id,
			Title: texts.T("help.article." + id + ".title"),
			Text:  texts.T("help.article." + id + ".text"),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Вопросы по результату
// ---------------------------------------------------------------------------

// Answer — ответ на вопрос по отчёту.
type Answer struct {
	Topic       string   `json:"topic"`       // тема, которую распознал бот: «Сертификаты и документы»
	Text        string   `json:"text"`        // ответ (разметка **жирный**)
	Found       bool     `json:"found"`       // тема распознана
	Suggestions []string `json:"suggestions"` // примеры вопросов
}

// topic — тема вопроса: ключевые слова и функция, собирающая ответ из отчёта.
type topic struct {
	id       string
	title    string
	keywords []string
	answer   func(rep report.Report) string
}

// Порядок важен: при равном числе совпадений побеждает тема выше по списку.
// Поэтому «Сколько ждать регистрацию?» — это «регистрация», а не «сроки».
var topics = []topic{
	{"documents", "Сертификаты и документы", []string{"сертификат", "документ", "разрешение", "фитосанитарный", "ветеринарный", "ветсертификат", "фитосертификат", "происхождение", "справка"}, answerDocuments},
	{"duty", "Таможенная пошлина", []string{"пошлина", "платеж", "платить", "заплатить", "сбор", "стоимость", "стоит", "деньги", "тариф"}, answerDuty},
	{"registration", "Регистрация производителя", []string{"регистрация", "зарегистрировать", "реестр", "gacc", "cifer", "цербер"}, answerRegistration},
	{"lab", "Лабораторные испытания", []string{"лаборатория", "испытание", "анализ", "исследование", "протокол", "тест", "проба"}, answerLab},
	{"labeling", "Маркировка", []string{"маркировка", "этикетка", "язык", "надпись", "штрихкод"}, answerBlock(report.BLabeling)},
	{"packaging", "Упаковка", []string{"упаковка", "тара", "мешок", "коробка", "поддон", "фасовка"}, answerBlock(report.BPackaging)},
	{"vat", "НДС и налоги", []string{"ндс", "налог", "бухгалтер", "возмещение", "вычет", "валютный"}, answerVAT},
	{"rate", "Курс валюты", []string{"курс", "валюта", "евро", "доллар", "юань", "цб"}, answerRate},
	{"restrictions", "Запреты, квоты и ограничения", []string{"запрет", "ограничение", "квота", "эмбарго", "санкции", "нельзя", "можно"}, answerRestrictions},
	{"logistics", "Логистика", []string{"логистика", "транспорт", "контейнер", "перевозка", "маршрут", "доставка", "рефрижератор", "температура", "транзит", "граница"}, answerLogistics},
	{"terms", "Сроки", []string{"срок", "ждать", "долго", "дней", "когда", "время"}, answerTerms},
	{"product", "Требования к продукции", []string{"качество", "показатель", "стандарт", "гост", "gb", "влажность", "требование", "норма"}, answerBlock(report.BProduct)},
}

// Suggestions — примеры вопросов (ТЗ §22, сценарий 1: вопросы тренера).
var Suggestions = []string{"Какие сертификаты нужны?", "Сколько пошлина?", "Сколько ждать регистрацию?", "Какие анализы сделать?", "Что указать на этикетке?"}

// Ask отвечает на вопрос по отчёту.
func Ask(rep report.Report, question string) Answer {
	words := engine.Words(question)
	best, bestScore := -1, 0
	for i, t := range topics {
		score := 0
		for _, w := range words {
			for _, k := range t.keywords {
				if engine.WordsMatch(w, engine.Normalize(k)) {
					score++
					break
				}
			}
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return Answer{Text: texts.T("ask.not_found"), Suggestions: Suggestions}
	}
	t := topics[best]
	r := rep.Result
	head := fmt.Sprintf("**%s** — %s → %s %s", t.title, r.Product.Name, r.Country.Flag, r.Country.Name)
	return Answer{
		Topic:       t.title,
		Text:        head + "\n" + t.answer(rep) + "\n\n" + texts.T("ask.footer"),
		Found:       true,
		Suggestions: Suggestions,
	}
}

// ---------------------------------------------------------------------------
// Ответы по темам
// ---------------------------------------------------------------------------

func bullets(lines []string) string {
	if len(lines) == 0 {
		return "— в отчёте нет данных по этому вопросу"
	}
	return "— " + strings.Join(lines, "\n— ")
}

func answerBlock(id string) func(report.Report) string {
	return func(rep report.Report) string {
		if b, ok := rep.Block(id); ok {
			return bullets(b.Lines)
		}
		return stopOr(rep, "— в отчёте нет данных по этому вопросу")
	}
}

// stopOr — если экспорт запрещён, главное в ответе — сам запрет.
func stopOr(rep report.Report, fallback string) string {
	if rep.Result.Stop != nil {
		return "🚫 " + rep.Result.Stop.Text
	}
	return fallback
}

func answerDocuments(rep report.Report) string {
	var need, no []string
	for _, d := range rep.Result.Req.Documents {
		if d.Status == data.DocNotRequired {
			no = append(no, d.Name)
		} else {
			need = append(need, report.DocLine(d))
		}
	}
	out := "Нужно оформить:\n" + bullets(need)
	if len(no) > 0 {
		out += "\nНе требуются: " + strings.Join(no, "; ") + "."
	}
	return stopOr(rep, out)
}

func answerDuty(rep report.Report) string {
	if b, ok := rep.Block(report.BDuty); ok {
		var keep []string
		for _, l := range b.Lines {
			if strings.HasPrefix(l, "НДС") || strings.HasPrefix(l, "Декларация") {
				continue // это — к теме «НДС»
			}
			keep = append(keep, l)
		}
		return bullets(keep)
	}
	return stopOr(rep, "")
}

func answerRegistration(rep report.Report) string {
	r := rep.Result
	for _, w := range r.Warnings {
		if w.Code == engine.WRegistration {
			return "— " + w.Text
		}
	}
	return fmt.Sprintf("— Для этого товара регистрация производителя в реестре %s не требуется (по данным справочника на %s).",
		r.Country.Registry.Name, engine.FormatDate(r.DataAsOf))
}

func answerLab(rep report.Report) string { return answerBlock(report.BLab)(rep) }

func answerVAT(rep report.Report) string {
	tax := rep.Result.Country.Tax
	lines := []string{"НДС: " + tax.VAT, "Декларация: " + tax.Declaration, "Бухгалтеру ВЭД: " + rep.Result.Country.Roles.Accountant}
	return bullets(lines)
}

func answerRate(rep report.Report) string {
	r := rep.Result
	if r.Country.EAEU {
		return "— Курс валюты не требуется: при экспорте в страны ЕАЭС пошлина не применяется."
	}
	if b, ok := rep.Block(report.BDuty); ok {
		for _, l := range b.Lines {
			if strings.HasPrefix(l, "Курс") {
				return "— " + l
			}
		}
	}
	return "— Курс валюты для этой ставки не требуется."
}

func answerRestrictions(rep report.Report) string {
	var lines []string
	r := rep.Result
	if r.Stop != nil {
		lines = append(lines, "🚫 "+r.Stop.Text)
	}
	for _, w := range r.Warnings {
		switch w.Code {
		case engine.WImportBan, engine.WQuota, engine.WComponent:
			lines = append(lines, w.Level.Icon()+" "+w.Text)
		}
	}
	if len(lines) == 0 {
		return fmt.Sprintf("— Запретов, квот и ограничений для этого товара и страны в справочнике нет (данные на %s). Перед сделкой проверьте актуальный статус на сайтах ФТС и Минсельхоза.",
			engine.FormatDate(r.DataAsOf))
	}
	return strings.Join(lines, "\n")
}

func answerLogistics(rep report.Report) string {
	r := rep.Result
	var lines []string
	for _, w := range r.Warnings {
		switch w.Code {
		case engine.WReefer, engine.WContainers, engine.WSmallBatch:
			lines = append(lines, w.Text)
		}
	}
	lines = append(lines, r.Country.Logistics...)
	return bullets(lines)
}

func answerTerms(rep report.Report) string {
	r := rep.Result
	var lines []string
	if r.Req.Registration {
		lines = append(lines, fmt.Sprintf("Регистрация производителя в реестре %s: %s мес. (ориентировочно)", r.Country.Registry.Name, r.Country.Registry.Months))
	}
	if r.Req.LabDays[1] > 0 {
		lines = append(lines, fmt.Sprintf("Лабораторные испытания: %d–%d рабочих дней", r.Req.LabDays[0], r.Req.LabDays[1]))
	}
	for _, d := range r.Req.Documents {
		if d.Term != "" && d.Status != data.DocNotRequired {
			lines = append(lines, d.Name+": "+d.Term)
		}
	}
	return stopOr(rep, bullets(lines))
}
