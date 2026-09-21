package engine

import (
	"fmt"
	"math"
	"strings"
	"time"

	"maxexport/data"
)

// ---------------------------------------------------------------------------
// Предупреждения (ТЗ §14): тексты взяты из ТЗ, в квадратные скобки подставлены данные.
// ---------------------------------------------------------------------------

// Level — важность предупреждения. От неё зависит значок в отчёте.
type Level int

const (
	Info Level = iota // ℹ️ информация
	Warn              // ⚠️ внимание
	Stop              // 🚫 экспорт невозможен
)

// Icon — значок уровня.
func (l Level) Icon() string {
	switch l {
	case Stop:
		return "🚫"
	case Warn:
		return "⚠️"
	default:
		return "ℹ️"
	}
}

// Warning — одно предупреждение. Code нужен тестам и демо-сценариям.
type Warning struct {
	Code  string
	Level Level
	Text  string
}

// Коды предупреждений.
const (
	WExportBan       = "EXPORT_BAN"
	WImportBan       = "IMPORT_BAN"
	WCodeReplaced    = "CODE_REPLACED"
	WCodeNote        = "CODE_NOTE"
	WCodeMismatch    = "CODE_MISMATCH"
	WBorderline      = "BORDERLINE"
	WComponent       = "COMPONENT"
	WQuota           = "QUOTA"
	WRegistration    = "REGISTRATION"
	WCertNotRecog    = "CERT_NOT_RECOGNIZED"
	WLabTime         = "LAB_TIME"
	WUnitWeight      = "UNIT_WEIGHT"
	WGrossNet        = "GROSS_NET"
	WReefer          = "REEFER"
	WTransit         = "TRANSIT"
	WWeightSkipped   = "WEIGHT_SKIPPED"
	WContainers      = "CONTAINERS"
	WSmallBatch      = "SMALL_BATCH"
	WRateFailed      = "RATE_FAILED"
	WRateJump        = "RATE_JUMP"
	WRateValidity    = "RATE_VALIDITY"
	WDataStale       = "DATA_STALE"
	WProfileFallback = "PROFILE_FALLBACK"
	WHumanCheck      = "HUMAN_CHECK"
)

// StaleAfterDays — через сколько дней после обновления справочников бот предупреждает,
// что данные могли устареть (ТЗ §14.1).
const StaleAfterDays = 30

// RateRecheckDays — если до отгрузки больше стольких дней, расчёт стоит обновить (ТЗ §14.1).
const RateRecheckDays = 14

// RateJumpPct — изменение курса, начиная с которого бот предупреждает (ТЗ §14.5).
const RateJumpPct = 3.0

// exportBan проверяет временный запрет вывоза из РФ (ТЗ §14.2).
func (e *Engine) exportBan(r Result) *Warning {
	day := r.ShipOrToday()
	for _, b := range e.Cat.Measures.ExportBans {
		if !hasPrefix(r.Product.Code, b.Prefixes) || !activeOn(b.From, b.To, day) {
			continue
		}
		applies := !r.Country.EAEU // без списка стран — запрет на вывоз за пределы ЕАЭС
		if len(b.Countries) > 0 {
			applies = contains(b.Countries, r.Country.ID)
		}
		if !applies {
			continue
		}
		text := fmt.Sprintf("Экспорт «%s» %s временно ограничен с %s (основание — %s). Подача декларации невозможна до снятия ограничения.",
			r.Product.Name, r.Country.To, isoToRu(b.From), b.Basis)
		if b.Training {
			text += " (учебный пример)"
		}
		return &Warning{Code: WExportBan, Level: Stop, Text: text}
	}
	return nil
}

// warnings собирает блок «⚠️ Внимание» в фиксированном порядке: сначала то, что мешает
// экспорту, затем данные о коде, документы, вес и логистика, курс и актуальность данных.
func (e *Engine) warnings(r Result, env Env) []Warning {
	var w []Warning
	add := func(code string, lvl Level, format string, args ...any) {
		w = append(w, Warning{Code: code, Level: lvl, Text: fmt.Sprintf(format, args...)})
	}
	p, c, in := r.Product, r.Country, r.In
	day := r.ShipOrToday()
	today := Day(r.At)

	// 1. Страна назначения запретила ввоз (ТЗ §14.2).
	for _, b := range e.Cat.Measures.ImportBans {
		if b.Country == c.ID && hasPrefix(p.Code, b.Prefixes) && activeOn(b.From, b.To, day) {
			text := fmt.Sprintf("%s — с %s (основание — %s). Рекомендую уточнить сроки снятия ограничения у импортёра или таможенного брокера.",
				b.Name, isoToRu(b.From), b.Basis)
			if b.Training {
				text += " (учебный пример)"
			}
			w = append(w, Warning{Code: WImportBan, Level: Warn, Text: text})
		}
	}

	// 2. Код заменён (ТЗ §14.1).
	if in.ReplacedFrom != "" {
		old := e.Cat.Product(in.ReplacedFrom)
		since := ""
		if old != nil && old.ReplacedBy != nil {
			since = isoToRu(old.ReplacedBy.Since)
		}
		add(WCodeReplaced, Warn, "Код %s заменён на %s с %s. Расчёт выполнен по новому коду. Проверьте, нужно ли обновить код в контракте и транспортных документах.",
			FormatCode(in.ReplacedFrom), FormatCode(p.Code), since)
	}
	if p.Note != "" {
		add(WCodeNote, Info, "%s", p.Note)
	}

	// 3. Код не соответствует введённому типу продукции (ТЗ §14.4).
	if in.ManualCode && strings.TrimSpace(in.Query) != "" {
		if _, ok := scoreProduct(p, Words(in.Query), strings.Join(Words(in.Query), " ")); !ok {
			add(WCodeMismatch, Warn, "Код %s относится к категории «%s», а указанный тип — «%s». Возможна ошибка в выборе кода. Проверьте классификацию или уточните тип продукции.",
				FormatCode(p.Code), p.Name, strings.TrimSpace(in.Query))
		}
	}

	// 4. Продукт на границе категорий (ТЗ §14.7).
	if len(p.Alternatives) > 0 {
		lines := []string{fmt.Sprintf("Продукт «%s» может быть отнесён к разным кодам ТН ВЭД:", p.Name)}
		for _, code := range append([]string{p.Code}, p.Alternatives...) {
			lines = append(lines, "• "+e.altLine(code, c))
		}
		if p.AltNote != "" {
			lines = append(lines, p.AltNote)
		}
		lines = append(lines, "Окончательное решение принимает таможенный орган. Рекомендую получить классификационное решение ФТС заранее.")
		add(WBorderline, Warn, "%s", strings.Join(lines, "\n"))
	}

	// 5. Компонент с отдельными ограничениями (ТЗ §14.7).
	for _, comp := range p.Components {
		if contains(comp.Countries, c.ID) {
			add(WComponent, Warn, "Продукт содержит компонент «%s», на который в стране назначения действуют отдельные ограничения (%s). Проверьте состав и соответствие требованиям по каждому компоненту.",
				comp.Name, comp.Note)
		}
	}

	// 6. Экспортная квота (ТЗ §14.2) — только при вывозе за пределы ЕАЭС.
	if !c.EAEU {
		for _, q := range e.Cat.Measures.Quotas {
			if !hasPrefix(p.Code, q.Prefixes) {
				continue
			}
			period := fmt.Sprintf("%s–%s", mmddToRu(q.From), mmddToRu(q.To))
			weight := "вес не указан"
			if in.WeightKg > 0 {
				weight = FormatKg(in.WeightKg)
			}
			text := fmt.Sprintf("На код ТН ВЭД установлена экспортная квота — %s т на период %s (%d г.; %s). Указанный вес партии (%s) может потребовать выделения индивидуальной квоты. Проверьте наличие квоты у компании-экспортёра. Сверх квоты — %s.",
				FormatNum(q.Tonnes), period, q.Year, q.Basis, weight, q.OverNote)
			if !inYearlyPeriod(q.From, q.To, day) {
				text += fmt.Sprintf(" На дату %s квота не действует — если отгрузка придётся на период %s, квота понадобится.", FormatDate(day), period)
			}
			add(WQuota, Warn, "%s", text)
		}
	}

	// 7. Регистрация производителя (ТЗ §14.3, §20.4).
	if r.Req.Registration {
		origin := map[data.Origin]string{data.OriginAnimal: "животного", data.OriginPlant: "растительного", data.OriginMixed: "смешанного"}[p.Origin]
		kind := "продукция"
		if origin != "" {
			kind = "продукция " + origin + " происхождения"
		}
		add(WRegistration, Warn, "Для экспорта %s %s требует регистрации предприятия-производителя в реестре %s. Без регистрации партия не будет пропущена таможней назначения. Срок регистрации — %s мес. (ориентировочно; зависит от продукции и полноты документов; подаёт: %s).",
			c.To, kind, c.Registry.Name, c.Registry.Months, c.Registry.Who)
	}

	// 8. Сертификаты ЕАЭС не признаются (ТЗ §14.3).
	if !c.EAEUCertsRecognized {
		add(WCertNotRecog, Warn, "Сертификат (декларация) соответствия ЕАЭС не признаётся в стране назначения (%s) для данного кода ТН ВЭД. Требуется подтвердить соответствие национальным стандартам страны. Аккредитованные лаборатории и порядок — уточните: %s.",
			c.Name, c.Authority)
	}

	// 9. Испытания не успеют до отгрузки (ТЗ §14.3).
	if !in.ShipDate.IsZero() && r.Req.LabDays[1] > 0 {
		left := BusinessDaysBetween(today, in.ShipDate)
		if left < r.Req.LabDays[1] {
			add(WLabTime, Warn, "Стандартный срок лабораторных испытаний для «%s» — %d–%d раб. дней. До указанной даты отгрузки (%s) остаётся %d раб. дн. Рекомендую ускорить отбор проб или сдвинуть дату отгрузки.",
				p.Name, r.Req.LabDays[0], r.Req.LabDays[1], FormatDate(in.ShipDate), left)
		}
	}

	// 10. Вес: нетипичный вес единицы, брутто/нетто, пропущенный вес (ТЗ §14.4, §15).
	if in.WeightKg > 0 {
		if uc := CheckUnitWeight(p, in.Qty, in.WeightKg, true); uc.Atypical {
			add(WUnitWeight, Warn, "Указанный вес одной единицы (%s кг) выходит за типичный диапазон для «%s» (обычно %s–%s кг). Проверьте данные.",
				FormatNum(uc.PerUnitKg), p.Name, FormatNum(p.UnitKg[0]), FormatNum(p.UnitKg[1]))
		}
		if in.NetKg > 0 && p.TarePct[1] > 0 {
			if t := TarePct(in.WeightKg, in.NetKg); t < p.TarePct[0] || t > p.TarePct[1] {
				add(WGrossNet, Warn, "Разница между весом брутто и нетто составляет %s %% — это нетипично для «%s» (обычно %s–%s %%). Проверьте, правильно ли указан вес тары. Таможня может запросить пояснение.",
					FormatNum(t), p.Name, FormatNum(p.TarePct[0]), FormatNum(p.TarePct[1]))
			}
		}
	} else {
		if !c.EAEU && r.Duty.Type != data.DutyNone {
			add(WWeightSkipped, Warn, "Для расчёта пошлины по ставке «%s» укажите вес. Расчёт выполнен без учёта весовой ставки.", DutyTypeName(r.Duty.Type))
		} else {
			add(WWeightSkipped, Info, "Вес не указан: проверки количества контейнеров и размера партии не выполнялись.")
		}
	}

	// 11. Логистика: температурный режим, маршрут, контейнеры, мелкая партия (ТЗ §14.6, §14.10).
	if t := p.TempC; t != nil {
		text := fmt.Sprintf("«%s» требует соблюдения температурного режима (от %s до %s °C) при транспортировке. Убедитесь, что перевозчик предоставит рефконтейнер или изотермический транспорт. Нарушение температурного режима может привести к отказу во ввозе.",
			p.Name, signed(t.Min), signed(t.Max))
		if t.Note != "" {
			text += " " + t.Note
		}
		add(WReefer, Warn, "%s", text)
	}
	if len(c.Logistics) > 0 {
		add(WTransit, Info, "%s", c.Logistics[0]) // первая строка — главное о маршруте; все строки — в .txt
	}
	if in.WeightKg > Container20MaxKg {
		n := ContainersNeeded(in.WeightKg)
		add(WContainers, Info, "Вес партии (%s кг) превышает вместимость одного 20-футового контейнера (~24 т). Потребуется %d %s (20 футов) или соответствующее число 40-футовых. Учтите это при расчёте логистических затрат.",
			FormatNum(in.WeightKg), n, Plural(int64(n), "контейнер", "контейнера", "контейнеров"))
	}
	if in.WeightKg > 0 && in.WeightKg < SmallBatchKg {
		add(WSmallBatch, Info, "Указанный вес (%s кг) относится к мелкой партии. Для мелких отправлений может применяться упрощённый порядок декларирования, но требования к сертификации и маркировке сохраняются в полном объёме.",
			FormatNum(in.WeightKg))
	}

	// 12. Курс валюты (ТЗ §14.5, §14.8) — нужен только при вывозе за пределы ЕАЭС.
	if !c.EAEU {
		if env.Rates.Failed {
			add(WRateFailed, Warn, "Не удалось получить актуальный курс валюты. Расчёт выполнен по последнему известному курсу на %s. Проверьте курс вручную перед подачей декларации.",
				FormatDate(env.Rates.Date))
		}
		if prev := env.Prev; prev != nil && r.Duty.RateRub > 0 && prev.Currency == r.Duty.Currency && prev.Rub > 0 {
			if pct := (r.Duty.RateRub - prev.Rub) / prev.Rub * 100; math.Abs(pct) >= RateJumpPct {
				add(WRateJump, Warn, "Курс %s изменился на %s %% с момента последнего расчёта (%s). Сумма пошлины пересчитана. Если отгрузка планируется через несколько дней, проверьте курс повторно.",
					r.Duty.Currency, signed(math.Round(pct*10)/10), FormatDate(prev.Date))
			}
		}
		text := fmt.Sprintf("Ставка пошлины актуальна на дату расчёта (%s). При подаче декларации таможня применит ставку и курс ЦБ РФ, действующие на день регистрации декларации.", FormatDate(today))
		if !in.ShipDate.IsZero() && Day(in.ShipDate).Sub(today) > RateRecheckDays*24*time.Hour {
			text += fmt.Sprintf(" До отгрузки больше %d дней — обновите расчёт перед подачей декларации.", RateRecheckDays)
		}
		add(WRateValidity, Warn, "%s", text)
	}

	// 13. Актуальность данных и доступность профиля страны (ТЗ §14.1, §14.8).
	if env.Flags.DataStale || (!r.DataAsOf.IsZero() && today.Sub(r.DataAsOf) > StaleAfterDays*24*time.Hour) {
		add(WDataStale, Warn, "Данные обновлены на %s. Не могу подтвердить, что с %s не вступили в силу новые требования. Рекомендую проверить актуальность на сайте ЕЭК или у таможенного брокера.",
			FormatDate(r.DataAsOf), FormatDate(r.DataAsOf))
	}
	if r.ProfileFallback {
		add(WProfileFallback, Warn, "Профиль требований для страны «%s» временно недоступен. Базовые требования приведены выше, но рекомендую уточнить детали у таможенного брокера. Приносим извинения за неудобство.", c.Name)
	}

	// 14. Что обязательно проверяет человек (ТЗ §20) — одной строкой.
	check := "Требует проверки специалиста: "
	if !in.ManualCode {
		check += "код ТН ВЭД определён автоматически, окончательную классификацию подтверждает таможенный орган; "
	} else {
		check += "окончательную классификацию по ТН ВЭД подтверждает таможенный орган; "
	}
	check += fmt.Sprintf("требования и ограничения приведены по состоянию на %s — проверьте их актуальность (%s).", FormatDate(r.DataAsOf), c.Authority)
	add(WHumanCheck, Info, "%s", check)
	return w
}

// altLine — одна строка варианта классификации: код, ставка пошлины, группа требований.
func (e *Engine) altLine(code string, c *data.Country) string {
	p := e.Cat.Product(code)
	if p == nil {
		return FormatCode(code)
	}
	rate := "пошлина не применяется (ЕАЭС)"
	if !c.EAEU {
		if d := e.FindDuty(code); d != nil {
			rate = "ставка пошлины: " + rateText(d)
		} else {
			rate = "ставка пошлины: не установлена (0 %)"
		}
	}
	return fmt.Sprintf("%s — %s; %s", FormatCode(code), p.Name, rate)
}

// ---------------------------------------------------------------------------
// Вспомогательные функции для дат и префиксов
// ---------------------------------------------------------------------------

func hasPrefix(code string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(code, p) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// activeOn — действует ли мера на дату day (from/to в формате "2006-01-02", to может быть пустым).
func activeOn(from, to string, day time.Time) bool {
	if f, ok := ParseISODate(from); ok && day.Before(f) {
		return false
	}
	if t, ok := ParseISODate(to); ok && day.After(t) {
		return false
	}
	return true
}

// inYearlyPeriod — попадает ли дата в ежегодный период "MM-DD"–"MM-DD" (например, квота на зерно).
func inYearlyPeriod(from, to string, day time.Time) bool {
	md := day.Format("01-02")
	return md >= from && md <= to
}

func isoToRu(s string) string {
	if t, ok := ParseISODate(s); ok {
		return FormatDate(t)
	}
	return s
}

func mmddToRu(s string) string {
	if len(s) == 5 {
		return s[3:] + "." + s[:2]
	}
	return s
}

func signed(v float64) string {
	if v > 0 {
		return "+" + FormatNum(v)
	}
	return FormatNum(v)
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) > 1 && r[1] >= 'а' && r[1] <= 'я' { // не трогаем аббревиатуры: «МАПП», «КНР»
		r[0] = []rune(strings.ToLower(string(r[0])))[0]
	}
	return string(r)
}
