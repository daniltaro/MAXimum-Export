package engine

import (
	"math"
	"strings"
	"testing"
	"time"

	"maxexport/data"
)

// Все тесты считают, что «сегодня» — 21.09.2026, 12:00 по Москве.
var now = time.Date(2026, 9, 21, 12, 0, 0, 0, MSK)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	return New(data.MustLoad())
}

func env() Env { return Env{Now: now, Rates: Training(now)} }

// firstWithPrefix — первый код справочника с таким началом (чтобы тесты не зависели от 10-го знака).
func firstWithPrefix(t *testing.T, e *Engine, prefix string) *data.Product {
	t.Helper()
	ps := e.Cat.ProductsWithPrefix(prefix)
	if len(ps) == 0 {
		t.Fatalf("в справочнике нет кода с началом %s", prefix)
	}
	return ps[0]
}

func hasWarning(r Result, code string) bool {
	for _, w := range r.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func warningText(r Result, code string) string {
	for _, w := range r.Warnings {
		if w.Code == code {
			return w.Text
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Форматирование и разбор ввода
// ---------------------------------------------------------------------------

func TestFormat(t *testing.T) {
	cases := map[float64]string{250000: "250 000", 725.7: "725,7", 0.005: "0,005", 5000000: "5 000 000", 12.5: "12,5", 0: "0"}
	for in, want := range cases {
		if got := FormatNum(in); got != want {
			t.Errorf("FormatNum(%v) = %q, want %q", in, got, want)
		}
	}
	if got := FormatCode("1701121000"); got != "1701 12 100 0" {
		t.Errorf("FormatCode = %q", got)
	}
	if got := Plural(5000, "мешок", "мешка", "мешков"); got != "мешков" {
		t.Errorf("Plural(5000) = %q", got)
	}
	if got := Plural(22, "мешок", "мешка", "мешков"); got != "мешка" {
		t.Errorf("Plural(22) = %q", got)
	}
	if got := Plural(11, "мешок", "мешка", "мешков"); got != "мешков" {
		t.Errorf("Plural(11) = %q", got)
	}
}

func TestParseQuantity(t *testing.T) {
	q, err := ParseQuantity("5 000 мешков (по 50 кг)")
	if err != nil || q.N != 5000 || q.UnitWord != "мешков" || q.PerUnitKg != 50 {
		t.Errorf("ParseQuantity = %+v, %v", q, err)
	}
	for _, bad := range []string{"0", "-5", "−3", "abc", "2,5"} {
		if _, err := ParseQuantity(bad); err == nil {
			t.Errorf("ParseQuantity(%q) должна вернуть ошибку", bad)
		}
	}
}

func TestParseWeight(t *testing.T) {
	cases := []struct {
		in       string
		kg, net  float64
		explicit bool
	}{
		{"250000", 250000, 0, false},
		{"250 000 кг", 250000, 0, true},
		{"250 т", 250000, 0, true},
		{"250т", 250000, 0, true},
		{"250,5 тонн", 250500, 0, true},
		{"250000 / 248000", 250000, 248000, false},
		{"250 т / 248", 250000, 248000, true},
	}
	for _, c := range cases {
		w, err := ParseWeight(c.in)
		if err != nil || w.Kg != c.kg || w.NetKg != c.net || w.Explicit != c.explicit {
			t.Errorf("ParseWeight(%q) = %+v, %v; want kg=%v net=%v explicit=%v", c.in, w, err, c.kg, c.net, c.explicit)
		}
	}
	for _, bad := range []string{"0", "-10", "нет", "100 / 200"} {
		if _, err := ParseWeight(bad); err == nil {
			t.Errorf("ParseWeight(%q) должна вернуть ошибку", bad)
		}
	}
}

func TestParseDate(t *testing.T) {
	d, ok := ParseDate("15.11.2026", now)
	if !ok || FormatDate(d) != "15.11.2026" {
		t.Errorf("ParseDate = %v %v", d, ok)
	}
	d, ok = ParseDate("01.02", now) // 1 февраля уже прошло → следующий год
	if !ok || FormatDate(d) != "01.02.2027" {
		t.Errorf("ParseDate(01.02) = %v", FormatDate(d))
	}
	if _, ok := ParseDate("31.02.2027", now); ok {
		t.Error("31.02 — несуществующая дата")
	}
}

func TestPII(t *testing.T) {
	for _, s := range []string{"мой телефон +7 (999) 123-45-67", "пишите на ivan@mail.ru", "ИНН 7701234567", "договор № 15/2026"} {
		if DetectPII(s) == "" {
			t.Errorf("не распознаны персональные данные в %q", s)
		}
	}
	for _, s := range []string{"сахар-песок", "5000 мешков", "250000", "1701 12 100 0", "какие нужны сертификаты?"} {
		if k := DetectPII(s); k != "" {
			t.Errorf("ложное срабатывание (%s) на %q", k, s)
		}
	}
}

// ---------------------------------------------------------------------------
// Поиск (ТЗ §15, экран 3; §22)
// ---------------------------------------------------------------------------

func codes(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Product.Code)
	}
	return out
}

func TestSearchScenarioQueries(t *testing.T) {
	e := newEngine(t)
	cases := []struct{ query, first string }{
		{"сахар-песок", "1701121000"},     // сценарий 1
		{"мёд натуральный", "0409000000"}, // сценарий 2
		{"мед натуральный", "0409000000"},
	}
	for _, c := range cases {
		hits := e.SearchProducts(c.query, now)
		if len(hits) == 0 || hits[0].Product.Code != c.first {
			t.Errorf("поиск %q: первым ожидался %s, получено %v", c.query, c.first, codes(hits))
		}
		if len(hits) > 5 {
			t.Errorf("поиск %q: найдено %d кодов — больше 5, бот попросит уточнить", c.query, len(hits))
		}
	}

	// Сценарий 3: запрос с опечаткой всё равно находит муку 1101 00 00 00.
	hits := e.SearchProducts("мука пшениичная", now)
	if !strings.Contains(strings.Join(codes(hits), ","), "1101000000") {
		t.Errorf("поиск с опечаткой не нашёл 1101000000: %v", codes(hits))
	}

	// «сахар» — слишком общий запрос: больше 5 вариантов (ТЗ §15: «уточните тип продукции»).
	if n := len(e.SearchProducts("сахар", now)); n <= 5 {
		t.Errorf("по запросу «сахар» найдено %d кодов, ожидалось больше 5", n)
	}

	// Не находим лишнего.
	if hits := e.SearchProducts("ноутбук", now); len(hits) != 0 {
		t.Errorf("не пищевые товары не должны находиться: %v", codes(hits))
	}
	if hits := e.SearchProducts("мука", now); strings.Contains(strings.Join(codes(hits), ","), "1207") {
		t.Errorf("«мука» не должна находить мак: %v", codes(hits))
	}
}

func TestMatchCountry(t *testing.T) {
	e := newEngine(t)
	cases := []struct {
		text  string
		id    string
		exact bool
	}{
		{"Китай", "cn", true},
		{"КНР", "cn", false},
		{"в Китая", "cn", false},
		{"Казахтан", "kz", false},
		{"армения", "am", true},
		{"Турция", "", false},
		{"Германия", "", false},
	}
	for _, c := range cases {
		m := e.MatchCountry(c.text)
		id := ""
		if m.Country != nil {
			id = m.Country.ID
		}
		if id != c.id || (id != "" && m.Exact != c.exact) {
			t.Errorf("MatchCountry(%q) = %q exact=%v, want %q exact=%v", c.text, id, m.Exact, c.id, c.exact)
		}
	}
}

func TestCheckCode(t *testing.T) {
	e := newEngine(t)
	if c := e.CheckCode("1701 12 100 0", now); c.Status != CodeOK {
		t.Errorf("код из сценария 1: статус %v", c.Status)
	}
	if c := e.CheckCode("1101 00 00 00", now); c.Status != CodeOK {
		t.Errorf("код из сценария 3 в записи ТЗ: статус %v", c.Status)
	}
	if c := e.CheckCode("9999 99 999 9", now); c.Status != CodeNotFound {
		t.Errorf("несуществующий код: статус %v", c.Status)
	}
	if c := e.CheckCode("8471300000", now); c.Status != CodeNonFood {
		t.Errorf("ноутбук: статус %v, ожидался «не пищевая продукция»", c.Status)
	}
	if c := e.CheckCode("0403101100", now); c.Status != CodeReplaced || c.Product.Code != "0403201100" {
		t.Errorf("устаревший код йогурта: %+v", c)
	}
	if c := e.CheckCode("1701", now); c.Status != CodePrefix || len(c.Candidates) == 0 {
		t.Errorf("4 цифры: ожидался список кодов, получено %+v", c.Status)
	}
	if c := e.CheckCode("17", now); c.Status != CodeBadFormat {
		t.Errorf("2 цифры: статус %v", c.Status)
	}
}

func TestUnitWeight(t *testing.T) {
	e := newEngine(t)
	flour := e.Cat.Product("1101000000")
	// Сценарий 3: 1000 мешков и вес 5 → 0,005 кг на мешок — нетипично.
	c := CheckUnitWeight(flour, 1000, 5, false)
	if !c.Atypical || !c.TonnesLikely {
		t.Errorf("вес 5 кг на 1000 мешков муки: %+v", c)
	}
	// Сахар: 5000 мешков, 250000 кг → 50 кг на мешок — типично.
	if c := CheckUnitWeight(e.Cat.Product("1701121000"), 5000, 250000, false); c.Atypical {
		t.Errorf("50 кг на мешок сахара — типичный вес: %+v", c)
	}
	// «250» при 5000 мешках: скорее всего тонны (ТЗ §15, экран 4).
	if c := CheckUnitWeight(e.Cat.Product("1701121000"), 5000, 250, false); !c.TonnesLikely {
		t.Errorf("250 при 5000 мешках должно предлагать тонны: %+v", c)
	}
	if ContainersNeeded(250000) != 11 {
		t.Errorf("250 т — 11 контейнеров, получено %d", ContainersNeeded(250000))
	}
}

// ---------------------------------------------------------------------------
// Пошлина (ТЗ §13) — числа из примеров ТЗ
// ---------------------------------------------------------------------------

func TestDutyFormulasFromSpec(t *testing.T) {
	e := newEngine(t)
	cn := e.Cat.Country("cn")
	// Условный товар: 250 т по 20 000 ₽/т → таможенная стоимость 5 000 000 ₽, как в примере ТЗ.
	p := &data.Product{Code: "9999999999", PriceRubPerT: 20000}
	rates := Training(now)

	e.Cat.Measures.Duties = append(e.Cat.Measures.Duties,
		data.Duty{Prefix: "9999999999", Type: data.DutySpecific, Amount: 25, Currency: "EUR", PerKg: 1000})
	d := e.CalcDuty(p, cn, 250000, rates)
	if d.TotalRub != 625000 {
		t.Errorf("2б. специфическая: 250 000 ÷ 1000 × 25 × 100 = 625 000 ₽, получено %v (%v)", d.TotalRub, d.Lines)
	}

	e.Cat.Measures.Duties[len(e.Cat.Measures.Duties)-1] =
		data.Duty{Prefix: "9999999999", Type: data.DutyAdValorem, AdValorem: 5}
	if d := e.CalcDuty(p, cn, 250000, rates); d.TotalRub != 250000 {
		t.Errorf("2а. адвалорная: 5 000 000 × 5 %% = 250 000 ₽, получено %v", d.TotalRub)
	}

	e.Cat.Measures.Duties[len(e.Cat.Measures.Duties)-1] =
		data.Duty{Prefix: "9999999999", Type: data.DutyCombined, AdValorem: 5, Amount: 15, Currency: "EUR", PerKg: 1000}
	d = e.CalcDuty(p, cn, 250000, rates)
	if d.TotalRub != 375000 || !strings.Contains(d.Winner, "по весу") {
		t.Errorf("2в. комбинированная: max(250 000; 375 000) = 375 000 ₽ по весу, получено %v %q", d.TotalRub, d.Winner)
	}

	// Без веса специфическую часть посчитать нельзя.
	if d := e.CalcDuty(p, cn, 0, rates); d.Complete {
		t.Error("без веса комбинированная пошлина не может быть посчитана полностью")
	}
}

func TestDutyRealRecords(t *testing.T) {
	e := newEngine(t)
	cn, am := e.Cat.Country("cn"), e.Cat.Country("am")
	sugar := e.Cat.Product("1701121000")
	if d := e.CalcDuty(sugar, cn, 250000, Training(now)); d.Type != data.DutyNone || d.TotalRub != 0 {
		t.Errorf("сахар в Китай: ожидалась пошлина «не установлена», 0 ₽; получено %v %v", d.Type, d.TotalRub)
	}
	if d := e.CalcDuty(sugar, am, 250000, Training(now)); !d.EAEU {
		t.Error("Армения — ЕАЭС, пошлина не применяется")
	}
	wheat := firstWithPrefix(t, e, "1001")
	if d := e.CalcDuty(wheat, cn, 100000, Training(now)); d.Type != data.DutySpecific || d.TotalRub <= 0 {
		t.Errorf("пшеница в Китай: ожидалась специфическая пошлина > 0, получено %v %v", d.Type, d.TotalRub)
	}
	rapeseed := firstWithPrefix(t, e, "1205")
	if d := e.CalcDuty(rapeseed, cn, 100000, Training(now)); d.Type != data.DutyCombined || d.Currency != "EUR" || !d.Complete {
		t.Errorf("рапс: ожидалась комбинированная ставка в евро: %+v", d)
	}
}

// Таможенный сбор за декларирование (ПП РФ № 1637): фиксированный или по шкале.
func TestCustomsFee(t *testing.T) {
	e := newEngine(t)
	cn := e.Cat.Country("cn")
	// Сахар: пошлины нет → фиксированный сбор 8 262 ₽.
	if d := e.CalcDuty(e.Cat.Product("1701121000"), cn, 250000, Training(now)); d.FeeRub != 8262 {
		t.Errorf("сахар: сбор %v, ожидалось 8 262 ₽", d.FeeRub)
	}
	// Рапс 100 т: комбинированная ставка → сбор по шкале от таможенной стоимости.
	rapeseed := firstWithPrefix(t, e, "1205")
	d := e.CalcDuty(rapeseed, cn, 100000, Training(now))
	var want float64
	for _, l := range e.Cat.Measures.CustomsFees.Scale {
		if l.UpToRub == 0 || d.CustomsValueRub <= l.UpToRub {
			want = l.FeeRub
			break
		}
	}
	if d.FeeRub != want || d.FeeRub == 0 {
		t.Errorf("рапс: стоимость %v, сбор %v, ожидалось %v", d.CustomsValueRub, d.FeeRub, want)
	}
	// 4,3 млн ₽ больше ступени «до 4,2 млн» → ступень «до 5,5 млн» = 21 344 ₽.
	if rapeseed.PriceRubPerT == 43000 && d.FeeRub != 21344 {
		t.Errorf("рапс 100 т × 43 000 ₽ = 4,3 млн ₽ → сбор 21 344 ₽, получено %v", d.FeeRub)
	}
	// ЕАЭС: сборов нет.
	if d := e.CalcDuty(rapeseed, e.Cat.Country("kz"), 100000, Training(now)); d.FeeRub != 0 {
		t.Errorf("ЕАЭС: сбор должен быть 0, получено %v", d.FeeRub)
	}
}

// ---------------------------------------------------------------------------
// Полный расчёт: 3 сценария ТЗ §22
// ---------------------------------------------------------------------------

func TestScenario1ChinaSugar(t *testing.T) {
	e := newEngine(t)
	r := e.Calculate(Input{Country: "cn", Code: "1701121000", Qty: 5000, UnitWord: "мешков", WeightKg: 250000}, env())
	if r.Stop != nil {
		t.Fatalf("экспорт сахара не запрещён: %v", r.Stop.Text)
	}
	if r.Duty.EAEU || r.Duty.TotalRub != 0 {
		t.Errorf("пошлина: %+v", r.Duty)
	}
	for _, code := range []string{WRegistration, WCertNotRecog, WRateValidity, WContainers} {
		if !hasWarning(r, code) {
			t.Errorf("нет предупреждения %s", code)
		}
	}
	if !strings.Contains(warningText(r, WRegistration), "GACC") || !strings.Contains(warningText(r, WRegistration), "2–6") {
		t.Errorf("ожидалась регистрация в GACC 2–6 мес.: %q", warningText(r, WRegistration))
	}
	if !strings.Contains(warningText(r, WContainers), "11 контейнеров") {
		t.Errorf("250 т — 11 контейнеров: %q", warningText(r, WContainers))
	}
	if len(r.Req.Packaging) == 0 || len(r.Req.Lab) == 0 || len(r.Req.Documents) == 0 {
		t.Error("пустые требования Китая к сахару")
	}
}

func TestScenario2ArmeniaHoney(t *testing.T) {
	e := newEngine(t)
	r := e.Calculate(Input{Country: "am", Code: "0409000000", Qty: 200, WeightKg: 4000}, env())
	if !r.Duty.EAEU {
		t.Error("Армения — ЕАЭС: пошлина не применяется")
	}
	if hasWarning(r, WRateValidity) || hasWarning(r, WRateFailed) {
		t.Error("для ЕАЭС курс не нужен — предупреждений о курсе быть не должно")
	}
	if !hasWarning(r, WRegistration) {
		t.Error("мёд — продукция животного происхождения: нужна регистрация в реестре ЕАЭС")
	}
	if !docMentions(r.Req.Documents, "ветеринар", data.DocRequired) {
		t.Errorf("мёд в Армению: нужен ветеринарный сертификат, документы: %+v", r.Req.Documents)
	}
}

func TestScenario3KazakhstanFlour(t *testing.T) {
	e := newEngine(t)
	r := e.Calculate(Input{Country: "kz", Code: "1101000000", Qty: 1000, WeightKg: 50000}, env())
	if !r.Duty.EAEU {
		t.Error("Казахстан — ЕАЭС")
	}
	if !docMentions(r.Req.Documents, "фитосанитар", data.DocRequired) {
		t.Errorf("мука: нужен фитосанитарный сертификат, документы: %+v", r.Req.Documents)
	}
	if docMentions(r.Req.Documents, "ветеринар", data.DocRequired) {
		t.Error("мука — растительная продукция: ветеринарный сертификат не нужен")
	}
	if hasWarning(r, WUnitWeight) {
		t.Error("50 кг на мешок муки — типичный вес")
	}
	// До исправления: вес 5 → предупреждение о нетипичном весе единицы.
	bad := e.Calculate(Input{Country: "kz", Code: "1101000000", Qty: 1000, WeightKg: 5, UnitWeightConfirmed: true}, env())
	if !strings.Contains(warningText(bad, WUnitWeight), "0,005 кг") {
		t.Errorf("ожидалось предупреждение о весе единицы: %q", warningText(bad, WUnitWeight))
	}
}

func docMentions(docs []data.Doc, word string, status data.DocStatus) bool {
	for _, d := range docs {
		if strings.Contains(strings.ToLower(d.Name), word) && d.Status == status {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Нестандартные ситуации (ТЗ §14)
// ---------------------------------------------------------------------------

func TestExportBan(t *testing.T) {
	e := newEngine(t)
	rice := firstWithPrefix(t, e, "100610")
	r := e.Calculate(Input{Country: "cn", Code: rice.Code, Qty: 20, WeightKg: 20000}, env())
	if r.Stop == nil || !strings.Contains(r.Stop.Text, "Подача декларации невозможна") {
		t.Errorf("рис-сырец в Китай: ожидался запрет экспорта, получено %+v", r.Stop)
	}
	// Запрет не действует внутри ЕАЭС.
	if r := e.Calculate(Input{Country: "kz", Code: rice.Code, Qty: 20, WeightKg: 20000}, env()); r.Stop != nil {
		t.Error("запрет вывоза за пределы ЕАЭС не должен действовать для Казахстана")
	}
}

func TestImportBanAndQuota(t *testing.T) {
	e := newEngine(t)
	wheat := firstWithPrefix(t, e, "1001")
	if r := e.Calculate(Input{Country: "kz", Code: wheat.Code, Qty: 100, WeightKg: 100000}, env()); !hasWarning(r, WImportBan) {
		t.Error("пшеница в Казахстан: ожидалось предупреждение о запрете ввоза")
	}
	r := e.Calculate(Input{Country: "cn", Code: wheat.Code, Qty: 100, WeightKg: 100000,
		ShipDate: time.Date(2027, 3, 1, 0, 0, 0, 0, MSK)}, env())
	if !hasWarning(r, WQuota) || strings.Contains(warningText(r, WQuota), "не действует") {
		t.Errorf("пшеница в Китай с отгрузкой 01.03.2027: квота должна действовать: %q", warningText(r, WQuota))
	}
}

func TestPoppyImportBanChina(t *testing.T) {
	e := newEngine(t)
	r := e.Calculate(Input{Country: "cn", Code: "1207919000", Qty: 10, WeightKg: 500}, env())
	if !strings.Contains(warningText(r, WImportBan), "мака") {
		t.Errorf("мак в Китай: ожидалось предупреждение о запрете ввоза, получено %q", warningText(r, WImportBan))
	}
	if r := e.Calculate(Input{Country: "kz", Code: "1207919000", Qty: 10, WeightKg: 500}, env()); hasWarning(r, WImportBan) {
		t.Error("запрет ввоза мака действует только в Китае")
	}
}

func TestCodeReplacedBeforeShipment(t *testing.T) {
	e := newEngine(t)
	for _, p := range e.Cat.Products {
		if p.ReplacedBy == nil {
			continue
		}
		since, _ := ParseISODate(p.ReplacedBy.Since)
		if !since.After(now) {
			continue
		}
		r := e.Calculate(Input{Country: "cn", Code: p.Code, Qty: 100, WeightKg: 1000, ShipDate: since.AddDate(0, 0, 10)}, env())
		if r.Product.Code != p.ReplacedBy.Code || !hasWarning(r, WCodeReplaced) {
			t.Errorf("код %s заменяется с %s: расчёт должен идти по новому коду", p.Code, p.ReplacedBy.Since)
		}
		return
	}
	t.Error("в справочнике нет примера кода, который заменится в будущем (ТЗ §14.1)")
}

func TestRateWarnings(t *testing.T) {
	e := newEngine(t)
	rapeseed := firstWithPrefix(t, e, "1205")
	in := Input{Country: "cn", Code: rapeseed.Code, Qty: 100, WeightKg: 100000}

	failed := env()
	failed.Rates.Failed = true
	failed.Rates.Date = now.AddDate(0, 0, -3)
	if r := e.Calculate(in, failed); !hasWarning(r, WRateFailed) {
		t.Error("ожидалось предупреждение о недоступном курсе")
	}

	jump := env()
	jump.Prev = &PrevRate{Currency: "EUR", Rub: 90, Date: now.AddDate(0, 0, -7)}
	if r := e.Calculate(in, jump); !hasWarning(r, WRateJump) {
		t.Error("курс евро 90 → 100 ₽: ожидалось предупреждение о скачке курса")
	}
}

func TestOtherEdgeCases(t *testing.T) {
	e := newEngine(t)
	sugar := "1701121000"

	// Мелкая партия.
	if r := e.Calculate(Input{Country: "cn", Code: sugar, Qty: 2, WeightKg: 50}, env()); !hasWarning(r, WSmallBatch) {
		t.Error("50 кг — мелкая партия")
	}
	// Брутто/нетто сильно различаются.
	if r := e.Calculate(Input{Country: "cn", Code: sugar, Qty: 5000, WeightKg: 250000, NetKg: 200000}, env()); !hasWarning(r, WGrossNet) {
		t.Error("тара 20 % для сахара — нетипично")
	}
	// Испытания не успевают до отгрузки.
	r := e.Calculate(Input{Country: "cn", Code: sugar, Qty: 5000, WeightKg: 250000, ShipDate: now.AddDate(0, 0, 3)}, env())
	if !hasWarning(r, WLabTime) {
		t.Error("отгрузка через 3 дня: испытания не успеют")
	}
	// Вес пропущен.
	if r := e.Calculate(Input{Country: "cn", Code: firstWithPrefix(t, e, "1001").Code, Qty: 100}, env()); !hasWarning(r, WWeightSkipped) || r.Duty.Complete {
		t.Error("без веса специфическая пошлина не считается и есть предупреждение")
	}
	// Код не соответствует типу продукции.
	if r := e.Calculate(Input{Country: "cn", Code: sugar, Query: "мёд", ManualCode: true, Qty: 1, WeightKg: 50}, env()); !hasWarning(r, WCodeMismatch) {
		t.Error("код сахара при типе «мёд» — ожидалось предупреждение о несоответствии")
	}
	// Рефрижератор.
	if r := e.Calculate(Input{Country: "cn", Code: firstWithPrefix(t, e, "2105").Code, Qty: 100, WeightKg: 1000}, env()); !hasWarning(r, WReefer) {
		t.Error("мороженое требует температурного режима")
	}
	// Профиль страны недоступен и устаревшие данные (демо-флаги).
	flags := env()
	flags.Flags = Flags{ProfileDown: true, DataStale: true}
	r = e.Calculate(Input{Country: "cn", Code: sugar, Qty: 5000, WeightKg: 250000}, flags)
	if !hasWarning(r, WProfileFallback) || !hasWarning(r, WDataStale) {
		t.Error("ожидались предупреждения о недоступном профиле и устаревших данных")
	}
}

func TestBorderlineAndComponents(t *testing.T) {
	e := newEngine(t)
	var border, comp bool
	for _, p := range e.Cat.Products {
		if len(p.Alternatives) > 0 && !border {
			border = hasWarning(e.Calculate(Input{Country: "cn", Code: p.Code, Qty: 10, WeightKg: 100}, env()), WBorderline)
		}
		for _, c := range p.Components {
			for _, country := range c.Countries {
				if !comp {
					comp = hasWarning(e.Calculate(Input{Country: country, Code: p.Code, Qty: 10, WeightKg: 100}, env()), WComponent)
				}
			}
		}
	}
	if !border {
		t.Error("нет рабочего примера спорной классификации (ТЗ §14.7)")
	}
	if !comp {
		t.Error("нет рабочего примера компонента с ограничениями (ТЗ §14.7)")
	}
}

// ТЗ §21: «Время отклика ≤ 5 секунд; 20 расчётов подряд — без сбоев».
func TestTwentyCalculationsFast(t *testing.T) {
	e := newEngine(t)
	start := time.Now()
	for i := 0; i < 20; i++ {
		for _, c := range []string{"cn", "am", "kz"} {
			for _, p := range e.Cat.Products {
				if p.Food {
					e.Calculate(Input{Country: c, Code: p.Code, Qty: int64(100 + i), WeightKg: float64(1000 * (i + 1))}, env())
				}
			}
		}
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("20 прогонов по всем кодам и странам заняли %v", d)
	}
}

// Ревью: длинное слово в поиске не должно замедлять бота (раньше 250 000 символов — 8 с).
func TestSearchLongInputFast(t *testing.T) {
	e := newEngine(t)
	start := time.Now()
	e.SearchProducts(strings.Repeat("z", 250000), now)
	e.SearchProducts(strings.Repeat("сахар ", 20000), now)
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Errorf("поиск по очень длинному запросу занял %v", d)
	}
}

func TestInputBounds(t *testing.T) {
	// Запись «1e306» в чате читается как число 1 (экспонента не поддерживается), а в API
	// такое значение отсекает CheckWeight — вместе с NaN и бесконечностью.
	if CheckWeight(1e306) != ErrWeightTooBig || CheckWeight(math.NaN()) != ErrWeightNonPos || CheckWeight(math.Inf(1)) != ErrWeightTooBig {
		t.Error("CheckWeight должен отклонять 1e306, NaN и бесконечность")
	}
	if _, err := ParseWeight("2000000000"); err != ErrWeightTooBig {
		t.Errorf("2 млрд кг: %v", err)
	}
	if _, err := ParseQuantity("5000000000"); err != ErrQtyTooBig {
		t.Errorf("5 млрд единиц: %v", err)
	}
	if got := ClipInput(strings.Repeat("я", 50), 40); len([]rune(got)) != 41 {
		t.Errorf("ClipInput: %q", got)
	}
}
