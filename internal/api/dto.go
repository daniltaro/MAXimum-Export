package api

// Здесь описан формат JSON, который отдаёт служебный API. Это внешний контракт:
// поля можно добавлять, но переименовывать и удалять — только вместе с openapi.yaml.
// Внутренние структуры ядра (engine, data) наружу напрямую не отдаются.

import (
	"time"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/report"
	"maxexport/internal/store"
)

// CountryDTO — страна назначения (экран 2).
type CountryDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Flag     string `json:"flag"`
	EAEU     bool   `json:"eaeu"`
	Regime   string `json:"regime"`   // «ЕАЭС» или «Третьи страны» — метка режима на экране 2
	Subtitle string `json:"subtitle"` // «Член ЕАЭС, пошлины не применяются»
}

func countryDTO(c *data.Country) CountryDTO {
	regime := "Третьи страны"
	if c.EAEU {
		regime = "ЕАЭС"
	}
	return CountryDTO{ID: c.ID, Name: c.Name, FullName: c.FullName, Flag: c.Flag, EAEU: c.EAEU, Regime: regime, Subtitle: c.Subtitle}
}

// ProductDTO — код ТН ВЭД из справочника (экран 3).
type ProductDTO struct {
	Code        string          `json:"code"`         // "1701121000"
	CodeDisplay string          `json:"code_display"` // "1701 12 100 0"
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Food        bool            `json:"food"`
	Unit        data.UnitForms  `json:"unit"`            // формы единицы по умолчанию: мешок / мешка / мешков
	UnitKg      [2]float64      `json:"typical_unit_kg"` // типичный вес единицы, кг
	Bulk        bool            `json:"bulk"`            // насыпной/наливной груз: количество — в тоннах
	TempC       *data.TempRange `json:"temp_c,omitempty"`
	Training    bool            `json:"training"` // учебная запись (например, код из ТЗ)
	Note        string          `json:"note,omitempty"`
}

func productDTO(p *data.Product) ProductDTO {
	return ProductDTO{
		Code: p.Code, CodeDisplay: engine.FormatCode(p.Code), Name: p.Name, Category: p.Category,
		Food: p.Food, Unit: p.Unit, UnitKg: p.UnitKg, Bulk: p.Unit.Many == "тонн", TempC: p.TempC,
		Training: p.Training, Note: p.Note,
	}
}

// SearchDTO — результат поиска кода по названию.
type SearchDTO struct {
	Query   string       `json:"query"`
	Items   []ProductDTO `json:"items"`    // не больше 5
	Total   int          `json:"total"`    // сколько совпадений всего
	TooMany bool         `json:"too_many"` // больше 5 — показать «Уточните тип продукции»
}

// CodeCheckDTO — проверка кода, введённого вручную.
type CodeCheckDTO struct {
	Status       string       `json:"status"`  // ok | bad_format | not_found | non_food | replaced | prefix
	Message      string       `json:"message"` // готовый текст для пользователя (кроме ok)
	Product      *ProductDTO  `json:"product,omitempty"`
	ReplacedFrom *ProductDTO  `json:"replaced_from,omitempty"` // для replaced — устаревший код
	Candidates   []ProductDTO `json:"candidates,omitempty"`    // для prefix — подходящие 10-значные коды
}

// WeightCheckRequest — проверка «вес ↔ количество» до расчёта.
type WeightCheckRequest struct {
	Code         string  `json:"code"`
	Quantity     int64   `json:"quantity"`
	WeightKg     float64 `json:"weight_kg"`
	ExplicitUnit bool    `json:"explicit_unit"` // пользователь явно выбрал кг или т — вариант «тонны» не предлагать
}

// WeightCheckDTO — результат проверки веса.
type WeightCheckDTO struct {
	PerUnitKg       float64    `json:"per_unit_kg"`
	TypicalUnitKg   [2]float64 `json:"typical_unit_kg"`
	Atypical        bool       `json:"atypical"`
	TonnesLikely    bool       `json:"tonnes_likely"` // спросить «это кг или тонны?»
	PerUnitIfTonnes float64    `json:"per_unit_if_tonnes,omitempty"`
	Message         string     `json:"message,omitempty"`
}

// DemoDTO — демо-режимы ведущего.
type DemoDTO struct {
	RateFail    bool    `json:"rate_fail"`
	RateJumpPct float64 `json:"rate_jump_pct"`
	ProfileDown bool    `json:"profile_down"`
	DataStale   bool    `json:"data_stale"`
}

// CalcRequestDTO — запрос расчёта (экраны 2–4).
type CalcRequestDTO struct {
	Country             string   `json:"country"`                 // cn | am | kz
	Code                string   `json:"code"`                    // 10 цифр, пробелы допустимы
	ProductQuery        string   `json:"product_query,omitempty"` // что пользователь искал — для проверки соответствия кода
	ManualCode          bool     `json:"manual_code,omitempty"`
	Quantity            int64    `json:"quantity"`
	UnitWord            string   `json:"unit_word,omitempty"`   // «мешков»; пусто — единица из справочника
	WeightKg            *float64 `json:"weight_kg,omitempty"`   // вес брутто; null — «Пропустить вес»
	NetKg               *float64 `json:"net_kg,omitempty"`      // вес нетто (необязательно)
	ShipDate            string   `json:"ship_date,omitempty"`   // "2026-11-15" (необязательно)
	UnitWeightConfirmed bool     `json:"unit_weight_confirmed"` // пользователь подтвердил нетипичный вес единицы
	PreviousID          string   `json:"previous_id,omitempty"` // прошлый расчёт — для предупреждения о скачке курса
	Demo                *DemoDTO `json:"demo,omitempty"`
}

// CalcDTO — результат расчёта (экран 5).
type CalcDTO struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"created_at"` // RFC 3339
	CalcDate    string `json:"calc_date"`  // «21.09.2026»
	DataAsOf    string `json:"data_as_of"` // дата актуальности справочников
	DownloadURL string `json:"download_url"`
	FileName    string `json:"file_name"`

	Params    ParamsDTO    `json:"params"`
	Stop      *WarningDTO  `json:"stop"`      // экспорт запрещён: показать вместо требований
	Blocks    []BlockDTO   `json:"blocks"`    // блоки отчёта в фиксированном порядке, готовые строки
	Documents []DocDTO     `json:"documents"` // чек-лист документов со статусами
	Duty      *DutyDTO     `json:"duty"`      // null — экспорт запрещён (stop), пошлину не показываем
	Rates     RatesDTO     `json:"rates"`
	Warnings  []WarningDTO `json:"warnings"`
	Roles     []RoleDTO    `json:"roles"`  // пусто при запрете экспорта
	Market    string       `json:"market"` // барьеры входа (для директора ВЭД)

	Disclaimer string   `json:"disclaimer"`
	ChatParts  []string `json:"chat_parts"` // тот же отчёт для чата (разметка **жирный**)
	CopyText   []string `json:"copy_text"`  // «Скопировать отчёт»: одна сводка ≤ 4000 символов (массив — для совместимости)
}

// ParamsDTO — параметры сделки (блок «📦 Параметры сделки»).
type ParamsDTO struct {
	Country      CountryDTO `json:"country"`
	Product      ProductDTO `json:"product"`
	Quantity     int64      `json:"quantity"`
	Unit         string     `json:"unit"`
	WeightKg     *float64   `json:"weight_kg"`
	NetKg        *float64   `json:"net_kg"`
	ShipDate     string     `json:"ship_date,omitempty"`
	ReplacedFrom string     `json:"replaced_from,omitempty"`
}

// BlockDTO — блок отчёта.
type BlockDTO struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Lines []string `json:"lines"`
}

// DocDTO — документ из чек-листа.
type DocDTO struct {
	Name   string `json:"name"`
	Status string `json:"status"` // required | conditional | no
	Who    string `json:"who,omitempty"`
	Term   string `json:"term,omitempty"`
	Note   string `json:"note,omitempty"`
}

// DutyDTO — расчёт пошлины.
type DutyDTO struct {
	Regime          string        `json:"regime"` // eaeu | third
	Type            string        `json:"type"`   // none | advalorem | specific | combined
	TypeName        string        `json:"type_name"`
	RateText        string        `json:"rate_text"`
	Basis           string        `json:"basis,omitempty"`
	ValidFrom       string        `json:"valid_from,omitempty"`
	ValidTo         string        `json:"valid_to,omitempty"`
	Note            string        `json:"note,omitempty"`
	Training        bool          `json:"training"`
	Lines           []DutyLineDTO `json:"lines"`
	TotalRub        float64       `json:"total_rub"`
	Complete        bool          `json:"complete"` // false — не хватило веса
	Winner          string        `json:"winner,omitempty"`
	CustomsValueRub float64       `json:"customs_value_rub,omitempty"`
	Currency        string        `json:"currency,omitempty"`
	RateRub         float64       `json:"rate_rub,omitempty"`
	FeeRub          float64       `json:"fee_rub"`
	FeeBasis        string        `json:"fee_basis,omitempty"`
	TotalWithFeeRub float64       `json:"total_with_fee_rub"`
	VAT             string        `json:"vat"`
	Declaration     string        `json:"declaration"`
	CustomsFeeNote  string        `json:"customs_fee_note,omitempty"` // для ЕАЭС: «сборы не взимаются»
}

// DutyLineDTO — строка формулы.
type DutyLineDTO struct {
	Label   string  `json:"label"`
	Formula string  `json:"formula"`
	Rub     float64 `json:"rub"`
}

// RatesDTO — курсы, использованные в расчёте.
type RatesDTO struct {
	Source string             `json:"source"` // training | cbr
	Date   string             `json:"date"`
	Failed bool               `json:"failed"` // свежий курс получить не удалось
	Values map[string]float64 `json:"values"`
}

// WarningDTO — предупреждение (ТЗ §14).
type WarningDTO struct {
	Code  string `json:"code"`
	Level string `json:"level"` // info | warn | stop
	Icon  string `json:"icon"`  // ℹ️ ⚠️ 🚫
	Text  string `json:"text"`
}

// RoleDTO — подсказка для роли (ТЗ §6, §21).
type RoleDTO struct {
	Role  string `json:"role"` // director | manager | customs | accountant | export_control
	Title string `json:"title"`
	Text  string `json:"text"`
}

// ErrorDTO — ошибка: {"error": {"code": "...", "field": "...", "message": "..."}}.
type ErrorDTO struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody — описание ошибки; message можно показывать пользователю как есть.
type ErrorBody struct {
	Code    string `json:"code"` // invalid_input | personal_data | not_found | too_large | bad_json | rate_limited | internal
	Field   string `json:"field,omitempty"`
	Kind    string `json:"kind,omitempty"` // для personal_data: что найдено («номер телефона», «e-mail»...)
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Преобразование результата расчёта в JSON
// ---------------------------------------------------------------------------

func calcDTO(c *store.Calc) CalcDTO {
	r, rep := c.Result, c.Report
	dto := CalcDTO{
		ID: c.ID, CreatedAt: c.Created.Format(time.RFC3339),
		CalcDate: engine.FormatDate(r.At), DataAsOf: engine.FormatDate(r.DataAsOf),
		DownloadURL: "/api/v1/calculations/" + c.ID + "/report.txt", FileName: rep.FileName(),
		Params: ParamsDTO{
			Country: countryDTO(r.Country), Product: productDTO(r.Product),
			Quantity: r.In.Qty, Unit: r.Unit(), WeightKg: optional(r.In.WeightKg), NetKg: optional(r.In.NetKg),
			ReplacedFrom: r.In.ReplacedFrom,
		},
		Market:     r.Req.Market,
		Disclaimer: report.Disclaimer,
		ChatParts:  rep.ChatParts(report.ChatLimit),
		CopyText:   []string{rep.Summary(report.CopyLimit)},
		Rates: RatesDTO{Source: r.Rates.Source, Date: engine.FormatDate(r.Rates.Date),
			Failed: r.Rates.Failed, Values: r.Rates.Values},
	}
	if !r.In.ShipDate.IsZero() {
		dto.Params.ShipDate = r.In.ShipDate.In(engine.MSK).Format("2006-01-02")
	}
	if r.Stop != nil {
		w := warningDTO(*r.Stop)
		dto.Stop = &w
	}
	for _, b := range rep.Blocks {
		dto.Blocks = append(dto.Blocks, BlockDTO{ID: b.ID, Title: b.Title, Lines: b.Lines})
	}
	// Предупреждения — по тому же правилу, что и в чате (report.VisibleWarnings).
	dto.Warnings = []WarningDTO{}
	for _, w := range report.VisibleWarnings(r) {
		dto.Warnings = append(dto.Warnings, warningDTO(w))
	}
	dto.Documents, dto.Roles = []DocDTO{}, []RoleDTO{}
	if r.Stop != nil {
		return dto // экспорт запрещён: вместо требований — только stop (ТЗ §15, экран 5)
	}
	for _, d := range r.Req.Documents {
		dto.Documents = append(dto.Documents, DocDTO{Name: d.Name, Status: string(d.Status), Who: d.Who, Term: d.Term, Note: d.Note})
	}
	roles := r.Country.Roles
	dto.Roles = []RoleDTO{
		{"director", "Директор по ВЭД", roles.Director},
		{"manager", "Менеджер по ВЭД", roles.Manager},
		{"customs", "Таможенный эксперт", roles.Customs},
		{"accountant", "Главный бухгалтер ВЭД", roles.Accountant},
		{"export_control", "Экспортный контроль", roles.ExportControl},
	}
	duty := dutyDTO(r)
	dto.Duty = &duty
	return dto
}

func dutyDTO(r engine.Result) DutyDTO {
	d, tax := r.Duty, r.Country.Tax
	out := DutyDTO{
		Regime: "third", Type: string(d.Type), TypeName: engine.DutyTypeName(d.Type), RateText: d.RateText,
		TotalRub: d.TotalRub, Complete: d.Complete, Winner: d.Winner, CustomsValueRub: d.CustomsValueRub,
		Currency: d.Currency, RateRub: d.RateRub, FeeRub: d.FeeRub, FeeBasis: d.FeeBasis,
		VAT: tax.VAT, Declaration: tax.Declaration, Lines: []DutyLineDTO{},
	}
	if d.EAEU {
		out.Regime, out.RateText, out.CustomsFeeNote = "eaeu", "не применяется (ЕАЭС)", tax.CustomsFee
	}
	if rec := d.Record; rec != nil {
		out.Basis, out.ValidFrom, out.ValidTo, out.Note, out.Training = rec.Basis, rec.ValidFrom, rec.ValidTo, rec.Note, rec.Training
	}
	for _, l := range d.Lines {
		out.Lines = append(out.Lines, DutyLineDTO{Label: l.Label, Formula: l.Formula, Rub: l.Rub})
	}
	if d.Complete {
		out.TotalWithFeeRub = d.TotalRub + d.FeeRub
	}
	return out
}

func warningDTO(w engine.Warning) WarningDTO {
	level := map[engine.Level]string{engine.Info: "info", engine.Warn: "warn", engine.Stop: "stop"}[w.Level]
	return WarningDTO{Code: w.Code, Level: level, Icon: w.Level.Icon(), Text: w.Text}
}

// optional — 0 превращается в null (вес не указан).
func optional(v float64) *float64 {
	if v <= 0 {
		return nil
	}
	return &v
}
