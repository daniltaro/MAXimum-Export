package data

import (
	"regexp"
	"strings"
	"testing"
)

// Проверка согласованности справочников. Запускайте после каждой правки JSON:
//
//	go test ./data/
var groups = map[string]bool{
	"sugar": true, "honey": true, "flour": true, "grain": true, "cereals": true, "oil": true,
	"oilseeds": true, "legumes": true, "dairy": true, "meat": true, "fish": true,
	"confectionery": true, "beverages": true,
}

var tenDigits = regexp.MustCompile(`^\d{10}$`)

func TestCatalogLoads(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Products) < 40 {
		t.Errorf("в справочнике ТН ВЭД всего %d кодов, ожидается не меньше 40", len(c.Products))
	}
	if len(c.Countries) != 3 {
		t.Fatalf("ожидается 3 страны, загружено %d", len(c.Countries))
	}
}

func TestProducts(t *testing.T) {
	c := MustLoad()
	for _, p := range c.Products {
		where := "tnved.json " + p.Code
		if !tenDigits.MatchString(p.Code) {
			t.Errorf("%s: код должен состоять из 10 цифр", where)
		}
		if p.Name == "" || p.Category == "" {
			t.Errorf("%s: не заполнены name или category", where)
		}
		if !p.Food {
			continue
		}
		if !groups[p.Group] {
			t.Errorf("%s: неизвестная группа %q", where, p.Group)
		}
		if len(p.Keywords) == 0 {
			t.Errorf("%s: нет ключевых слов для поиска", where)
		}
		if p.UnitKg[0] <= 0 || p.UnitKg[0] >= p.UnitKg[1] {
			t.Errorf("%s: unit_kg должен быть [мин, макс], мин > 0 и мин < макс: %v", where, p.UnitKg)
		}
		if p.TarePct[0] < 0 || p.TarePct[0] > p.TarePct[1] {
			t.Errorf("%s: некорректный tare_pct %v", where, p.TarePct)
		}
		if p.PriceRubPerT <= 0 {
			t.Errorf("%s: не задана учебная цена price_rub_per_t", where)
		}
		if p.Unit.One == "" || p.Unit.Few == "" || p.Unit.Many == "" {
			t.Errorf("%s: не заданы формы единицы unit", where)
		}
		if p.ReplacedBy != nil && c.Product(p.ReplacedBy.Code) == nil {
			t.Errorf("%s: replaced_by ссылается на отсутствующий код %s", where, p.ReplacedBy.Code)
		}
		for _, alt := range p.Alternatives {
			if c.Product(alt) == nil {
				t.Errorf("%s: alternatives ссылается на отсутствующий код %s", where, alt)
			}
		}
	}
}

func TestCountriesHaveAllGroups(t *testing.T) {
	c := MustLoad()
	for _, country := range c.Countries {
		for g := range groups {
			req, ok := country.Requirements(g, "")
			if !ok {
				t.Errorf("%s.json: нет профиля группы %q", country.ID, g)
				continue
			}
			if len(req.Documents) == 0 || len(req.Lab) == 0 || len(req.Labeling) == 0 {
				t.Errorf("%s.json, группа %s: пустые документы, лаборатория или маркировка", country.ID, g)
			}
			if req.LabDays[0] <= 0 || req.LabDays[0] > req.LabDays[1] {
				t.Errorf("%s.json, группа %s: некорректный lab_days %v", country.ID, g, req.LabDays)
			}
			for _, d := range req.Documents {
				switch d.Status {
				case DocRequired, DocConditional, DocNotRequired:
				default:
					t.Errorf("%s.json, группа %s: документ %q с неизвестным статусом %q", country.ID, g, d.Name, d.Status)
				}
			}
		}
		if country.Roles.Director == "" || country.Roles.Manager == "" || country.Roles.Customs == "" ||
			country.Roles.Accountant == "" || country.Roles.ExportControl == "" {
			t.Errorf("%s.json: заполнены не все 5 ролей (ТЗ §21)", country.ID)
		}
		if country.Tax.VAT == "" || country.Tax.Declaration == "" {
			t.Errorf("%s.json: не заполнены tax.vat или tax.declaration", country.ID)
		}
	}
}

func TestEveryFoodCodeHasDutyRecord(t *testing.T) {
	c := MustLoad()
	for _, p := range c.Products {
		if !p.Food {
			continue
		}
		found := false
		for _, d := range c.Measures.Duties {
			if strings.HasPrefix(p.Code, d.Prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("measures.json: для кода %s (%s) нет записи о пошлине — добавьте хотя бы \"none\"", p.Code, p.Name)
		}
	}
}

func TestMeasures(t *testing.T) {
	c := MustLoad()
	for _, d := range c.Measures.Duties {
		switch d.Type {
		case DutyNone:
		case DutyAdValorem:
			if d.AdValorem <= 0 {
				t.Errorf("пошлина %s: адвалорная ставка без процента", d.Prefix)
			}
		case DutySpecific, DutyCombined:
			if d.Currency == "" || d.PerKg <= 0 {
				t.Errorf("пошлина %s: у специфической части нет валюты или per_kg", d.Prefix)
			}
		default:
			t.Errorf("пошлина %s: неизвестный тип %q", d.Prefix, d.Type)
		}
	}
	for _, b := range c.Measures.ImportBans {
		if c.Country(b.Country) == nil {
			t.Errorf("запрет ввоза: неизвестная страна %q", b.Country)
		}
	}
}

// Если в одной группе разные товары (разные первые 6 цифр кода), общих строк группы
// недостаточно: у каждого такого товара должно быть уточнение в разделе products страны.
func TestMixedGroupsHaveProductOverrides(t *testing.T) {
	c := MustLoad()
	subheadings := map[string]map[string]bool{} // группа → набор 6-значных субпозиций
	for _, p := range c.Products {
		if p.Food && p.ReplacedBy == nil {
			if subheadings[p.Group] == nil {
				subheadings[p.Group] = map[string]bool{}
			}
			subheadings[p.Group][p.Code[:6]] = true
		}
	}
	for _, country := range c.Countries {
		for prefix := range country.Products {
			if len(c.ProductsWithPrefix(prefix)) == 0 {
				t.Errorf("%s.json: уточнение products[%q] не подходит ни к одному коду", country.ID, prefix)
			}
		}
		for _, p := range c.Products {
			if !p.Food || p.ReplacedBy != nil || len(subheadings[p.Group]) < 2 {
				continue
			}
			if o := country.productOverride(p.Code); o.Market == "" && len(o.Product) == 0 {
				t.Errorf("%s.json: у товара %s (%s) из смешанной группы %q нет уточнения в products (нужны хотя бы product и market)",
					country.ID, p.Code, p.Name, p.Group)
			}
		}
	}
}

func TestCustomsFees(t *testing.T) {
	f := MustLoad().Measures.CustomsFees
	if f.FlatRub <= 0 || len(f.Scale) == 0 || f.Basis == "" {
		t.Fatalf("measures.json: не заполнены customs_fees: %+v", f)
	}
	prev := 0.0
	for i, l := range f.Scale {
		last := i == len(f.Scale)-1
		if (l.UpToRub <= prev && !last) || (last && l.UpToRub != 0) {
			t.Errorf("шкала сборов: ступени должны возрастать, последняя — up_to_rub 0 («свыше»): %+v", f.Scale)
		}
		prev = l.UpToRub
	}
}
