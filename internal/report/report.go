// Package report собирает отчёт по результату расчёта в трёх видах:
//   - ChatParts — сообщения в чат (с разбивкой на «1/2», «2/2», если длиннее 3500 символов);
//   - Plain     — компактная версия без оформления для кнопки «📋 Скопировать отчёт»;
//   - Text      — текстовый документ .txt для кнопки «Скачать отчёт».
//
// Разметка в чате минимальная: **жирный** текст. Адаптер мессенджера сам превращает её
// в формат MAX (HTML), а веб-эмулятор — в HTML страницы.
package report

import (
	"fmt"
	"strings"

	"maxexport/data"
	"maxexport/internal/engine"
)

// Disclaimer — нижняя строка каждого результата: напоминание, что это прототип
// и перед подачей декларации данные нужно перепроверить.
const Disclaimer = "⚠️ Это демонстрационный прототип (MVP). Данные могут быть неактуальными. Результат не является официальной консультацией. Перед подачей декларации проверьте все параметры у таможенного брокера или в открытых источниках: ТКС ЕАЭС, сайт ЦБ РФ, сайт ФТС."

// Идентификаторы блоков — в том порядке, в котором они выводятся. Порядок фиксированный
// и одинаковый для чата, копии и файла: сначала параметры сделки, затем стоп-факторы,
// дальше требования (упаковка → продукция → маркировка → документы → лаборатория),
// затем деньги (пошлина), затем разбивка по ролям и в конце предупреждения.
const (
	BParams    = "params"
	BStop      = "stop"
	BPackaging = "packaging"
	BProduct   = "product"
	BLabeling  = "labeling"
	BDocuments = "documents"
	BLab       = "lab"
	BDuty      = "duty"
	BRoles     = "roles"
	BWarnings  = "warnings"
)

// Block — смысловой блок отчёта: заголовок со значком и маркированные строки.
type Block struct {
	ID    string
	Title string
	Lines []string
}

// Report — готовый отчёт.
type Report struct {
	Result engine.Result
	Blocks []Block
}

// Build собирает отчёт из результата расчёта.
func Build(r engine.Result) Report {
	rep := Report{Result: r}
	add := func(id, title string, lines []string) {
		if len(lines) > 0 {
			rep.Blocks = append(rep.Blocks, Block{ID: id, Title: title, Lines: lines})
		}
	}

	add(BParams, "📦 Параметры сделки", paramsLines(r))

	if r.Stop != nil {
		// Экспорт запрещён: остальные блоки не нужны, вместо них — одно крупное предупреждение.
		add(BStop, "🚫 Экспорт невозможен", []string{r.Stop.Text})
	} else {
		add(BPackaging, "✅ Упаковка", r.Req.Packaging)
		add(BProduct, "✅ Продукция", r.Req.Product)
		add(BLabeling, "✅ Маркировка", r.Req.Labeling)
		add(BDocuments, "📋 Сертификаты и разрешения", docLines(r))
		add(BLab, "🔬 Лабораторные испытания", labLines(r.Req))
		add(BDuty, "💰 Таможенная пошлина", dutyLines(r))
		add(BRoles, "👥 Кому что важно", roleLines(r))
	}
	add(BWarnings, "⚠️ Внимание", warningLines(r))
	return rep
}

// Block возвращает блок по идентификатору (для тестов и ответов на вопросы).
func (rep Report) Block(id string) (Block, bool) {
	for _, b := range rep.Blocks {
		if b.ID == id {
			return b, true
		}
	}
	return Block{}, false
}

// ---------------------------------------------------------------------------
// Содержимое блоков
// ---------------------------------------------------------------------------

func paramsLines(r engine.Result) []string {
	regime := "вне ЕАЭС"
	if r.Country.EAEU {
		regime = "ЕАЭС"
	}
	lines := []string{
		fmt.Sprintf("Страна: %s %s (%s)", r.Country.Flag, r.Country.Name, regime),
		"Продукт: " + r.Product.Name,
		"Код ТН ВЭД: " + engine.FormatCode(r.Product.Code),
		fmt.Sprintf("Количество: %s %s", engine.FormatInt(r.In.Qty), r.Unit()),
	}
	switch {
	case r.In.WeightKg <= 0:
		lines = append(lines, "Вес: не указан")
	case r.In.NetKg > 0:
		lines = append(lines, fmt.Sprintf("Вес: %s брутто, %s нетто", engine.FormatKg(r.In.WeightKg), engine.FormatKg(r.In.NetKg)))
	default:
		lines = append(lines, fmt.Sprintf("Вес: %s, брутто", engine.FormatKg(r.In.WeightKg)))
	}
	if !r.In.ShipDate.IsZero() {
		lines = append(lines, "Плановая отгрузка: "+engine.FormatDate(r.In.ShipDate))
	}
	lines = append(lines, "Дата расчёта: "+engine.FormatDate(r.At))
	return lines
}

func docLines(r engine.Result) []string {
	var out []string
	for _, d := range r.Req.Documents {
		out = append(out, DocLine(d))
	}
	return out
}

// DocLine — строка чек-листа документа: что, кто выдаёт, срок (для менеджера ВЭД).
func DocLine(d data.Doc) string {
	s := d.Name
	switch d.Status {
	case data.DocNotRequired:
		s += " — не требуется"
		if d.Note != "" {
			s += " (" + d.Note + ")"
		}
		return s
	case data.DocConditional:
		if d.Note != "" {
			s += " — " + d.Note
		} else {
			s += " — при необходимости"
		}
	default:
		if d.Note != "" {
			s += " — " + d.Note
		}
	}
	var extra []string
	if d.Who != "" {
		extra = append(extra, d.Who)
	}
	if d.Term != "" {
		extra = append(extra, "срок: "+d.Term)
	}
	if len(extra) > 0 {
		s += " (" + strings.Join(extra, "; ") + ")"
	}
	return s
}

func labLines(req data.Requirements) []string {
	out := append([]string{}, req.Lab...)
	if req.LabDays[1] > 0 {
		out = append(out, fmt.Sprintf("Ориентировочный срок: %d–%d рабочих дней", req.LabDays[0], req.LabDays[1]))
	}
	return out
}

func dutyLines(r engine.Result) []string {
	d, tax := r.Duty, r.Country.Tax
	if d.EAEU {
		// Внутри ЕАЭС вывозной пошлины нет, поэтому курс валюты не нужен — итог сразу 0 ₽.
		out := []string{"Таможенная пошлина: **не применяется (ЕАЭС)**"}
		if tax.DutyNote != "" {
			out = append(out, tax.DutyNote)
		}
		return append(out,
			"НДС: "+tax.VAT,
			"Декларация: "+tax.Declaration,
			"Курс валюты: не требуется (пошлина равна нулю)",
			tax.CustomsFee,
			"Итого к уплате: **0 ₽**",
		)
	}

	rec := d.Record
	if d.Type == data.DutyNone {
		// Для кода нет вывозной ставки: пошлина 0 %, но основание и примечание показываем.
		out := []string{
			"Тип ставки: не установлена (0 %)",
			"Экспортная пошлина для данного кода ТН ВЭД не установлена (ставка — 0 %). Пошлина к уплате — 0 ₽.",
		}
		if rec.Basis != "" {
			out = append(out, "Основание: "+rec.Basis)
		}
		if rec.Note != "" {
			out = append(out, rec.Note)
		}
		out = append(out, "Итого к уплате: **0 ₽**", feeLine(d), rateLine(r))
		return append(out, taxLines(r)...)
	}

	out := []string{"Тип ставки: " + engine.DutyTypeName(d.Type)}
	rate := "Ставка: " + d.RateText
	if rec.Basis != "" {
		rate += " — " + rec.Basis
	}
	if rec.ValidFrom != "" || rec.ValidTo != "" {
		rate += fmt.Sprintf("; действует %s–%s", isoToRu(rec.ValidFrom), isoToRu(rec.ValidTo))
	}
	if rec.Training {
		rate += " — учебная ставка"
	}
	out = append(out, rate)
	if rec.Note != "" {
		out = append(out, rec.Note)
	}
	for _, l := range d.Lines {
		out = append(out, l.Label+": "+l.Formula)
	}

	switch {
	case !d.Complete:
		out = append(out, "⚠️ Для расчёта пошлины по этой ставке укажите вес. Расчёт выполнен без учёта весовой ставки.")
	case d.Winner != "":
		out = append(out, fmt.Sprintf("Итого к уплате: **%s** (%s)", engine.FormatRub(d.TotalRub), d.Winner))
	default:
		out = append(out, "Итого к уплате: **"+engine.FormatRub(d.TotalRub)+"**")
	}
	out = append(out, feeLine(d))
	if d.Complete && d.FeeRub > 0 && d.TotalRub > 0 {
		out = append(out, "Всего таможенных платежей (пошлина + сбор): **"+engine.FormatRub(d.TotalRub+d.FeeRub)+"**")
	}

	out = append(out, rateLine(r))
	if r.Rates.Failed {
		out = append(out, fmt.Sprintf("⚠️ Курс загружен на %s. Проверьте актуальный курс перед подачей декларации.", engine.FormatDate(r.Rates.Date)))
	}
	return append(out, taxLines(r)...)
}

// taxLines — НДС и декларирование (для бухгалтера ВЭД).
func taxLines(r engine.Result) []string {
	return []string{"НДС: " + r.Country.Tax.VAT, "Декларация: " + r.Country.Tax.Declaration}
}

// feeLine — таможенный сбор за декларирование (вне ЕАЭС).
func feeLine(d engine.DutyResult) string {
	if d.FeeRub <= 0 {
		return "Таможенный сбор: " + d.FeeBasis
	}
	return "Таможенный сбор: " + engine.FormatRub(d.FeeRub) + " — " + d.FeeBasis
}

// rateLine — какой курс валюты взят для расчёта и откуда: с сайта ЦБ РФ или резервный
// из локальных данных. Пользователь должен видеть источник и дату курса.
func rateLine(r engine.Result) string {
	d := r.Duty
	switch {
	case d.Type == data.DutyNone:
		return "Курс валюты: не требуется (ставка 0 %)"
	case d.Currency == "" || d.Currency == "RUB":
		if d.Type == data.DutySpecific {
			return "Курс валюты: не требуется (ставка в рублях)"
		}
		return "Курс валюты: не требуется (стоимость оценена в рублях)"
	}
	sign := engine.CurrencySign(d.Currency)
	if r.Rates.Source == engine.SourceCBR {
		return fmt.Sprintf("Курс: 1 %s = %s ₽ — загружен с сайта ЦБ РФ на %s. Для таможенных платежей применяется курс ЦБ РФ на дату регистрации декларации.",
			sign, engine.FormatNum(d.RateRub), engine.FormatDate(r.Rates.Date))
	}
	return fmt.Sprintf("Курс: 1 %s = %s ₽ — учебный курс. В промышленной версии курс загружается с сайта ЦБ РФ; для декларации применяется курс на дату её регистрации.",
		sign, engine.FormatNum(d.RateRub))
}

func roleLines(r engine.Result) []string {
	roles := r.Country.Roles
	director := roles.Director
	if r.Req.Market != "" {
		director += " Рынок: " + r.Req.Market
	}
	return []string{
		"Директор по ВЭД: " + director,
		"Менеджер по ВЭД: " + roles.Manager,
		"Таможенный эксперт: " + roles.Customs,
		"Главный бухгалтер ВЭД: " + roles.Accountant,
		"Экспортный контроль: " + roles.ExportControl,
	}
}

// stopRelevant — предупреждения, которые показываются и при запрете экспорта.
var stopRelevant = map[string]bool{
	engine.WCodeNote: true, engine.WCodeReplaced: true, engine.WImportBan: true,
	engine.WDataStale: true, engine.WHumanCheck: true,
}

// VisibleWarnings — предупреждения, которые показываются в блоке «⚠️ Внимание».
// Одно правило для чата, мини-приложения и .txt.
func VisibleWarnings(r engine.Result) []engine.Warning {
	var out []engine.Warning
	for _, w := range r.Warnings {
		if w.Code == engine.WWeightSkipped && w.Level == engine.Warn {
			continue // уже показано в блоке пошлины
		}
		if r.Stop != nil && !stopRelevant[w.Code] {
			continue // экспорт запрещён — требования к документам и логистике сейчас не важны
		}
		out = append(out, w)
	}
	return out
}

func warningLines(r engine.Result) []string {
	var out []string
	for _, w := range VisibleWarnings(r) {
		out = append(out, w.Level.Icon()+" "+w.Text)
	}
	return out
}

func isoToRu(s string) string {
	if t, ok := engine.ParseISODate(s); ok {
		return engine.FormatDate(t)
	}
	return s
}
