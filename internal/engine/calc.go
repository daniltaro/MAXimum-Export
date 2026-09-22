package engine

import (
	"time"

	"maxexport/data"
)

// ---------------------------------------------------------------------------
// Полный расчёт: входные данные → требования + пошлина + предупреждения (ТЗ §12)
// ---------------------------------------------------------------------------

// Input — всё, что пользователь ввёл на экранах 2–4.
type Input struct {
	Country      string // "cn", "am", "kz"
	Code         string // 10 цифр
	Query        string // название товара, которое вводил пользователь (для проверки соответствия кода)
	ManualCode   bool   // код введён вручную, а не выбран из найденных
	ReplacedFrom string // устаревший код, который ввёл пользователь (если был заменён)

	Qty      int64
	UnitWord string // «мешков»; пусто — единица по умолчанию из справочника

	WeightKg float64   // вес брутто; 0 — пользователь нажал «Пропустить вес»
	NetKg    float64   // вес нетто (необязательно)
	ShipDate time.Time // плановая дата отгрузки (необязательно)

	UnitWeightConfirmed bool // пользователь подтвердил нетипичный вес единицы («Всё верно»)
}

// Flags — демо-режимы для ведущего (ТЗ §21: «показать 5 нестандартных сценариев»).
type Flags struct {
	ProfileDown bool // «справочник требований страны недоступен»
	DataStale   bool // «вступило в силу новое решение, ещё не загруженное в базу»
}

// PrevRate — курс при прошлом расчёте пользователя (для предупреждения о скачке курса).
type PrevRate struct {
	Currency string
	Rub      float64
	Date     time.Time
}

// Env — окружение расчёта: текущее время, курсы, история пользователя, демо-флаги.
type Env struct {
	Now   time.Time
	Rates Rates
	Prev  *PrevRate
	Flags Flags
}

// Result — результат расчёта, из которого собирается отчёт.
type Result struct {
	At      time.Time
	In      Input
	Product *data.Product
	Country *data.Country

	Req             data.Requirements
	ProfileFallback bool // профиль страны для этой группы товара недоступен

	Duty  DutyResult
	Rates Rates

	Stop     *Warning  // запрет экспорта: вместо требований — крупное предупреждение
	Warnings []Warning // блок «⚠️ Внимание»

	DataAsOf time.Time // дата актуальности справочников
}

// Calculate выполняет расчёт. Функция не обращается к сети и не зависит от часов
// компьютера — всё приходит в env, поэтому её легко проверять тестами.
func (e *Engine) Calculate(in Input, env Env) Result {
	res := Result{At: env.Now, In: in, Rates: env.Rates}
	res.Country = e.Cat.Country(in.Country)
	res.Product = e.Cat.Product(in.Code)
	res.DataAsOf, _ = ParseISODate(e.Cat.Measures.DataAsOf)

	// Код будет заменён до даты отгрузки — считаем по новому коду (ТЗ §14.1).
	if p := res.Product; p != nil && p.ReplacedBy != nil && !in.ShipDate.IsZero() {
		if since, ok := ParseISODate(p.ReplacedBy.Since); ok && !Day(in.ShipDate).Before(since) {
			if np := e.Cat.Product(p.ReplacedBy.Code); np != nil {
				res.In.ReplacedFrom, res.In.Code, res.Product = p.Code, np.Code, np
			}
		}
	}

	// Требования страны назначения (ТЗ §12: «загружает профиль требований»).
	var ok bool
	res.Req, ok = res.Country.Requirements(res.Product.Group, res.Product.Code)
	res.ProfileFallback = !ok || env.Flags.ProfileDown
	if env.Flags.ProfileDown {
		res.Req, _ = res.Country.Requirements("", "") // только общие требования страны
	}

	// Пошлина: для специфической ставки берём нетто, если оно указано, иначе брутто.
	base := in.WeightKg
	if in.NetKg > 0 {
		base = in.NetKg
	}
	res.Duty = e.CalcDuty(res.Product, res.Country, base, env.Rates)

	res.Stop = e.exportBan(res)
	res.Warnings = e.warnings(res, env)
	return res
}

// ShipOrToday — дата, на которую проверяются запреты и квоты: дата отгрузки или сегодня.
func (r Result) ShipOrToday() time.Time {
	if !r.In.ShipDate.IsZero() {
		return Day(r.In.ShipDate)
	}
	return Day(r.At)
}

// Unit — единица товара для отчёта: то, что написал пользователь, или единица из справочника.
func (r Result) Unit() string {
	if r.In.UnitWord != "" {
		return r.In.UnitWord
	}
	u := r.Product.Unit
	if u.Many == "" {
		return "ед."
	}
	return Plural(r.In.Qty, u.One, u.Few, u.Many)
}
