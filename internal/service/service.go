// Package service — операции, общие для чат-бота и мини-приложения: поиск кода,
// проверка ввода, расчёт с сохранением, вопросы по результату.
//
// Чат-бот (internal/flow) вызывает эти функции напрямую, мини-приложение — через
// HTTP API (internal/api). Поэтому правила проверки и расчёта в обоих интерфейсах одинаковые.
package service

import (
	"errors"
	"strings"
	"time"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/faq"
	"maxexport/internal/report"
	"maxexport/internal/store"
	"maxexport/texts"
)

// MaxSearchResults — сколько кодов показывать в списке (ТЗ §15, экран 3: «найдено более 5 кодов» → уточните).
const MaxSearchResults = 5

// Service — точка входа для интерфейсов.
type Service struct {
	Engine *engine.Engine
	Rates  engine.RateSource
	Store  *store.Store
	Now    func() time.Time // текущее время; в тестах подменяется
}

// New создаёт сервис: справочники, источник курса, хранилище расчётов на 24 часа.
func New(cat *data.Catalog, rates engine.RateSource) *Service {
	return &Service{
		Engine: engine.New(cat),
		Rates:  rates,
		Store:  store.New(24*time.Hour, 10000),
		Now:    time.Now,
	}
}

// InputError — ошибка во входных данных: какое поле и что сказать пользователю.
type InputError struct {
	Field   string // "country", "code", "quantity", "weight_kg", "net_kg", "ship_date"
	Message string
}

func (e *InputError) Error() string { return e.Message }

// ErrNotFound — расчёт не найден (устарел или программа перезапускалась).
var ErrNotFound = errors.New(texts.T("error.calc_not_found"))

// MaxInputEcho — сколько символов из ввода пользователя повторять в сообщении об ошибке.
const MaxInputEcho = 40

// NotYetValidMessage — «код начнёт действовать с …, пока используйте …» (ТЗ §14.1).
func NotYetValidMessage(c engine.CodeCheck) string {
	current := "—"
	if c.Current != nil {
		current = engine.FormatCode(c.Current.Code)
	}
	return texts.T("code.not_yet_valid", "code", engine.FormatCode(c.Product.Code), "date", engine.FormatDate(c.ValidFrom), "current", current)
}

// ---------------------------------------------------------------------------
// Справочная информация
// ---------------------------------------------------------------------------

// Countries — страны в порядке кнопок на экране 2.
func (s *Service) Countries() []data.Country { return s.Engine.Cat.Countries }

// SearchResult — найденные коды (не больше MaxSearchResults) и общее число совпадений.
type SearchResult struct {
	Items   []*data.Product
	Total   int
	TooMany bool // совпадений больше 5 — попросить уточнить (ТЗ §15, экран 3)
}

// Search ищет коды ТН ВЭД по названию товара.
func (s *Service) Search(query string) SearchResult {
	hits := s.Engine.SearchProducts(query, s.Now())
	res := SearchResult{Total: len(hits), TooMany: len(hits) > MaxSearchResults}
	for i, h := range hits {
		if i == MaxSearchResults {
			break
		}
		res.Items = append(res.Items, h.Product)
	}
	return res
}

// CheckCode проверяет код ТН ВЭД, введённый вручную.
func (s *Service) CheckCode(input string) engine.CodeCheck {
	return s.Engine.CheckCode(input, s.Now())
}

// CheckWeight проверяет, соответствует ли вес количеству (ТЗ §14.4, §14.9).
func (s *Service) CheckWeight(code string, qty int64, weightKg float64, explicitUnit bool) (engine.UnitCheck, error) {
	p := s.Engine.Cat.Product(code)
	if p == nil {
		return engine.UnitCheck{}, &InputError{"code", texts.T("code.not_found", "code", engine.FormatCode(code))}
	}
	if err := engine.CheckQuantity(qty); err != nil {
		return engine.UnitCheck{}, &InputError{"quantity", err.Error()}
	}
	if err := engine.CheckWeight(weightKg); err != nil {
		return engine.UnitCheck{}, &InputError{"weight_kg", err.Error()}
	}
	return engine.CheckUnitWeight(p, qty, weightKg, explicitUnit), nil
}

// ---------------------------------------------------------------------------
// Расчёт
// ---------------------------------------------------------------------------

// Demo — демо-режимы ведущего (ТЗ §21: «ведущий может показать 5 нестандартных сценариев»).
type Demo struct {
	RateFail    bool    // источник курса недоступен
	RateJumpPct float64 // курс изменился на столько % с прошлого расчёта
	ProfileDown bool    // профиль требований страны недоступен
	DataStale   bool    // данные могли устареть
	TnvedDown   bool    // справочник ТН ВЭД недоступен (проверяется в диалоге, не в расчёте)
}

// CalcRequest — данные для расчёта (экраны 2–4).
type CalcRequest struct {
	Country             string
	Code                string
	ReplacedFrom        string // устаревший код, который ввёл пользователь (расчёт — по новому)
	ProductQuery        string // что пользователь писал в поиске — для проверки соответствия кода
	ManualCode          bool
	Quantity            int64
	UnitWord            string
	WeightKg            float64 // 0 — «Пропустить вес»
	NetKg               float64
	ShipDate            time.Time
	UnitWeightConfirmed bool
	PreviousID          string // прошлый расчёт пользователя — для предупреждения о скачке курса
	Demo                Demo
}

// Calculate проверяет данные, выполняет расчёт, собирает отчёт и сохраняет его.
func (s *Service) Calculate(req CalcRequest) (*store.Calc, error) {
	now := s.Now()
	e := s.Engine

	country := e.Cat.Country(strings.ToLower(strings.TrimSpace(req.Country)))
	if country == nil {
		return nil, &InputError{"country", texts.T("country.unsupported", "input", engine.ClipInput(req.Country, MaxInputEcho))}
	}

	in := engine.Input{
		Country: country.ID, Query: req.ProductQuery, ManualCode: req.ManualCode,
		Qty: req.Quantity, UnitWord: req.UnitWord, WeightKg: req.WeightKg, NetKg: req.NetKg,
		ShipDate: req.ShipDate, UnitWeightConfirmed: req.UnitWeightConfirmed,
	}

	in.ReplacedFrom = req.ReplacedFrom
	switch c := e.CheckCode(req.Code, now); c.Status {
	case engine.CodeOK:
		in.Code = c.Product.Code
	case engine.CodeReplaced:
		in.Code, in.ReplacedFrom = c.Product.Code, c.Old.Code // ТЗ §15: расчёт по новому коду
	case engine.CodeNonFood:
		return nil, &InputError{"code", texts.T("code.non_food", "code", engine.FormatCode(c.Digits), "category", c.Product.Category)}
	case engine.CodeNotYetValid:
		return nil, &InputError{"code", NotYetValidMessage(c)}
	case engine.CodeNotFound, engine.CodePrefix:
		return nil, &InputError{"code", texts.T("code.not_found", "code", engine.FormatCode(c.Digits))}
	default:
		return nil, &InputError{"code", texts.T("code.bad_format", "input", engine.ClipInput(req.Code, MaxInputEcho))}
	}

	if err := engine.CheckQuantity(req.Quantity); err != nil {
		return nil, &InputError{"quantity", err.Error()}
	}
	if req.WeightKg != 0 { // 0 — «Пропустить вес»
		if err := engine.CheckWeight(req.WeightKg); err != nil {
			return nil, &InputError{"weight_kg", err.Error()}
		}
	}
	if req.NetKg != 0 {
		if err := engine.CheckWeight(req.NetKg); err != nil {
			return nil, &InputError{"net_kg", err.Error()}
		}
	}
	switch {
	case req.NetKg > 0 && req.WeightKg == 0:
		return nil, &InputError{"weight_kg", texts.T("weight.need_gross")}
	case req.NetKg > req.WeightKg && req.WeightKg > 0:
		return nil, &InputError{"net_kg", engine.ErrNetGtGross.Error()}
	case !req.ShipDate.IsZero() && engine.Day(req.ShipDate).Before(engine.Day(now)):
		return nil, &InputError{"ship_date", texts.T("review.date_past", "date", engine.FormatDate(req.ShipDate))}
	case !req.ShipDate.IsZero() && req.ShipDate.After(now.AddDate(2, 0, 0)):
		return nil, &InputError{"ship_date", texts.T("review.date_too_far", "date", engine.FormatDate(req.ShipDate))}
	}

	env := engine.Env{Now: now, Rates: s.Rates.Current(now), Flags: engine.Flags{
		ProfileDown: req.Demo.ProfileDown, DataStale: req.Demo.DataStale,
	}}
	if req.Demo.RateFail {
		env.Rates.Failed = true
		env.Rates.Date = engine.Day(now).AddDate(0, 0, -2) // «последний известный курс» — позавчерашний
	}
	env.Prev = s.previousRate(req, in.Code, env.Rates, now)

	res := e.Calculate(in, env)
	return s.Store.Put(res, report.Build(res)), nil
}

// previousRate — курс при прошлом расчёте (для предупреждения о скачке курса, ТЗ §14.5).
func (s *Service) previousRate(req CalcRequest, code string, rates engine.Rates, now time.Time) *engine.PrevRate {
	if req.Demo.RateJumpPct != 0 {
		// Демо: делаем вид, что неделю назад курс был другим.
		if d := s.Engine.FindDuty(code); d != nil && d.Currency != "" && d.Currency != "RUB" {
			if cur, ok := rates.Get(d.Currency); ok {
				return &engine.PrevRate{Currency: d.Currency, Rub: cur / (1 + req.Demo.RateJumpPct/100), Date: now.AddDate(0, 0, -7)}
			}
		}
	}
	if req.PreviousID != "" {
		if prev := s.Store.Get(req.PreviousID, now); prev != nil && prev.Result.Duty.RateRub > 0 {
			return &engine.PrevRate{Currency: prev.Result.Duty.Currency, Rub: prev.Result.Duty.RateRub, Date: prev.Result.At}
		}
	}
	return nil
}

// Get возвращает сохранённый расчёт.
func (s *Service) Get(id string) (*store.Calc, error) {
	if c := s.Store.Get(id, s.Now()); c != nil {
		return c, nil
	}
	return nil, ErrNotFound
}

// Ask отвечает на вопрос по сохранённому расчёту.
func (s *Service) Ask(id, question string) (faq.Answer, error) {
	c, err := s.Get(id)
	if err != nil {
		return faq.Answer{}, err
	}
	return faq.Ask(c.Report, question), nil
}
