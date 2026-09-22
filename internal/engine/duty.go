package engine

import (
	"strings"

	"maxexport/data"
)

// ---------------------------------------------------------------------------
// Расчёт экспортной пошлины (ТЗ §13)
// ---------------------------------------------------------------------------

// DutyLine — одна строка расчёта с формулой: «250 000 ÷ 1 000 × 25 € × 100 ₽ = 625 000 ₽».
type DutyLine struct {
	Label   string
	Formula string
	Rub     float64
}

// DutyResult — итог расчёта пошлины для блока «💰 Таможенная пошлина».
type DutyResult struct {
	EAEU     bool          // страна ЕАЭС — пошлина не применяется (ТЗ §13, сценарий 1)
	Record   *data.Duty    // запись из measures.json (nil для ЕАЭС)
	Type     data.DutyType // вид ставки
	RateText string        // ставка словами: «5 % от таможенной стоимости»
	Lines    []DutyLine    // формулы расчёта
	TotalRub float64       // итого к уплате
	Complete bool          // false — не хватило веса, итог посчитан не полностью

	CustomsValueRub float64 // учебная оценка таможенной стоимости (для адвалорной части)
	Currency        string  // валюта специфической ставки: "EUR", "USD", "RUB"
	RateRub         float64 // курс этой валюты, ₽
	Winner          string  // для комбинированной: «по весу» или «по стоимости»

	FeeRub   float64 // таможенный сбор за декларирование, ₽ (0 — посчитать нельзя)
	FeeBasis string  // как определён сбор
}

// FindDuty — запись о пошлине с самым длинным подходящим префиксом кода.
// Если записи нет — пошлина не установлена.
func (e *Engine) FindDuty(code string) *data.Duty {
	var best *data.Duty
	for i := range e.Cat.Measures.Duties {
		d := &e.Cat.Measures.Duties[i]
		if strings.HasPrefix(code, d.Prefix) && (best == nil || len(d.Prefix) > len(best.Prefix)) {
			best = d
		}
	}
	return best
}

// CalcDuty считает пошлину. baseKg — вес для специфической ставки (0 — вес не указан).
func (e *Engine) CalcDuty(p *data.Product, c *data.Country, baseKg float64, rates Rates) DutyResult {
	if c.EAEU {
		// Сценарий 1 из ТЗ §13: внутри ЕАЭС пошлин нет, курс не нужен.
		return DutyResult{EAEU: true, Type: data.DutyNone, Complete: true}
	}

	rec := e.FindDuty(p.Code)
	if rec == nil {
		rec = &data.Duty{Type: data.DutyNone, Name: "Экспортная пошлина не установлена"}
	}
	r := DutyResult{Record: rec, Type: rec.Type, Complete: true}
	r.RateText = rateText(rec)

	tonnes := baseKg / 1000
	hasWeight := baseKg > 0

	// Адвалорная часть: процент от таможенной стоимости. Стоимость контракта спрашивать
	// нельзя (ТЗ §17), поэтому берём учебную индикативную цену из справочника.
	adval := func() (DutyLine, bool) {
		if !hasWeight || p.PriceRubPerT <= 0 {
			return DutyLine{}, false
		}
		r.CustomsValueRub = tonnes * p.PriceRubPerT
		sum := r.CustomsValueRub * rec.AdValorem / 100
		r.Lines = append(r.Lines, DutyLine{
			Label:   "Таможенная стоимость (учебная оценка)",
			Formula: FormatNum(tonnes) + " т × " + FormatNum(p.PriceRubPerT) + " ₽/т = " + FormatRub(r.CustomsValueRub),
			Rub:     r.CustomsValueRub,
		})
		return DutyLine{
			Label:   "Расчёт по стоимости",
			Formula: FormatNum(r.CustomsValueRub) + " × " + FormatNum(rec.AdValorem) + " % = " + FormatRub(sum),
			Rub:     sum,
		}, true
	}

	// Специфическая часть: вес ÷ единица ставки × ставка × курс валюты ставки.
	specific := func() (DutyLine, bool) {
		if !hasWeight {
			return DutyLine{}, false
		}
		per := rec.PerKg
		if per <= 0 {
			per = 1000
		}
		rate, ok := rates.Get(rec.Currency)
		if !ok {
			return DutyLine{}, false
		}
		r.Currency, r.RateRub = currencyOrRub(rec.Currency), rate
		sum := baseKg / per * rec.Amount * rate
		f := FormatNum(baseKg) + " ÷ " + FormatNum(per) + " × " + FormatNum(rec.Amount) + " " + CurrencySign(rec.Currency)
		if r.Currency != "RUB" {
			f += " × " + FormatNum(rate) + " ₽"
		}
		return DutyLine{Label: "Расчёт по весу", Formula: f + " = " + FormatRub(sum), Rub: sum}, true
	}

	switch rec.Type {
	case data.DutyNone:
		r.Lines = append(r.Lines, DutyLine{Label: "Пошлина", Formula: "ставка 0 % — пошлина не взимается", Rub: 0})

	case data.DutyAdValorem:
		if l, ok := adval(); ok {
			r.Lines = append(r.Lines, l)
			r.TotalRub = l.Rub
		} else {
			r.Complete = false
		}

	case data.DutySpecific:
		if l, ok := specific(); ok {
			r.Lines = append(r.Lines, l)
			r.TotalRub = l.Rub
		} else {
			r.Complete = false
		}

	case data.DutyCombined:
		// Комбинированная ставка: считаем оба варианта и берём большую сумму (ТЗ §13, шаг 2в).
		la, okA := adval()
		ls, okS := specific()
		if !okA || !okS {
			r.Complete = false
			break
		}
		r.Lines = append(r.Lines, la, ls)
		if ls.Rub >= la.Rub {
			r.TotalRub, r.Winner = ls.Rub, "расчёт по весу даёт большую сумму"
		} else {
			r.TotalRub, r.Winner = la.Rub, "расчёт по стоимости даёт большую сумму"
		}
	}
	r.FeeRub, r.FeeBasis = e.customsFee(r)
	return r
}

// customsFee — таможенный сбор за декларирование при экспорте (для бухгалтера ВЭД).
// Без пошлины или со специфической пошлиной — фиксированная сумма; с адвалорной
// или комбинированной — по шкале от таможенной стоимости.
func (e *Engine) customsFee(r DutyResult) (float64, string) {
	f := e.Cat.Measures.CustomsFees
	if r.Type == data.DutyNone || r.Type == data.DutySpecific {
		return f.FlatRub, f.FlatNote + "; " + f.Basis
	}
	if r.CustomsValueRub <= 0 {
		return 0, "по шкале от таможенной стоимости — укажите вес, чтобы оценить стоимость; " + f.Basis
	}
	for _, l := range f.Scale {
		if l.UpToRub == 0 || r.CustomsValueRub <= l.UpToRub {
			return l.FeeRub, "по таможенной стоимости " + FormatRub(r.CustomsValueRub) + " (учебная оценка); " + f.Basis
		}
	}
	return 0, f.Basis
}

// rateText описывает ставку словами.
func rateText(d *data.Duty) string {
	per := d.PerKg
	if per <= 0 {
		per = 1000
	}
	spec := FormatNum(d.Amount) + " " + CurrencySign(d.Currency) + " за " + FormatNum(per) + " кг"
	switch d.Type {
	case data.DutyAdValorem:
		return FormatNum(d.AdValorem) + " % от таможенной стоимости"
	case data.DutySpecific:
		return spec
	case data.DutyCombined:
		return FormatNum(d.AdValorem) + " % от таможенной стоимости, но не менее " + spec
	default:
		return "не установлена (0 %)"
	}
}

// DutyTypeName — вид ставки по-русски (ТЗ §13, шаг 1).
func DutyTypeName(t data.DutyType) string {
	switch t {
	case data.DutyAdValorem:
		return "адвалорная"
	case data.DutySpecific:
		return "специфическая"
	case data.DutyCombined:
		return "комбинированная"
	default:
		return "не установлена"
	}
}

// CurrencySign — знак валюты: "EUR" → "€".
func CurrencySign(c string) string {
	switch c {
	case "EUR":
		return "€"
	case "USD":
		return "$"
	case "CNY":
		return "¥"
	default:
		return "₽"
	}
}

func currencyOrRub(c string) string {
	if c == "" {
		return "RUB"
	}
	return c
}
