package data

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// Файлы справочников вшиваются в программу при сборке — отдельно их копировать
// на сервер или в Docker-образ не нужно.
//
//go:embed tnved.json measures.json countries/*.json
var files embed.FS

// Catalog — все справочники, загруженные в память.
type Catalog struct {
	Products  []Product           // справочник ТН ВЭД в порядке файла
	Measures  Measures            // пошлины, квоты, запреты
	Countries []Country           // Китай, Армения, Казахстан — в порядке кнопок на экране 2
	byCode    map[string]*Product // быстрый поиск по коду
	byCountry map[string]*Country
}

// CountryOrder — порядок стран на экране выбора (ТЗ §15, экран 2).
var CountryOrder = []string{"cn", "am", "kz"}

// Load читает вшитые JSON-файлы. Ошибка означает, что справочник повреждён.
func Load() (*Catalog, error) {
	c := &Catalog{byCode: map[string]*Product{}, byCountry: map[string]*Country{}}

	if err := readJSON("tnved.json", &c.Products); err != nil {
		return nil, err
	}
	if err := readJSON("measures.json", &c.Measures); err != nil {
		return nil, err
	}
	for _, id := range CountryOrder {
		var country Country
		if err := readJSON(path.Join("countries", id+".json"), &country); err != nil {
			return nil, err
		}
		c.Countries = append(c.Countries, country)
	}

	for i := range c.Products {
		p := &c.Products[i]
		if _, dup := c.byCode[p.Code]; dup {
			return nil, fmt.Errorf("tnved.json: код %s встречается дважды", p.Code)
		}
		c.byCode[p.Code] = p
	}
	for i := range c.Countries {
		c.byCountry[c.Countries[i].ID] = &c.Countries[i]
	}
	return c, nil
}

// MustLoad — как Load, но при ошибке останавливает программу (справочники вшиты,
// поэтому ошибка возможна только при неправильно отредактированном JSON).
func MustLoad() *Catalog {
	c, err := Load()
	if err != nil {
		panic(err)
	}
	return c
}

func readJSON(name string, v any) error {
	raw, err := files.ReadFile(name)
	if err != nil {
		return fmt.Errorf("справочник %s: %w", name, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields() // опечатка в имени поля JSON — сразу ошибка, а не тихо пропущенные данные
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("справочник %s: %w", name, err)
	}
	return nil
}

// Product возвращает запись справочника по 10-значному коду (или nil).
func (c *Catalog) Product(code string) *Product { return c.byCode[code] }

// Country возвращает профиль страны по id ("cn", "am", "kz") или nil.
func (c *Catalog) Country(id string) *Country { return c.byCountry[id] }

// ProductsWithPrefix — все коды, начинающиеся с prefix (для ввода 4–8 цифр).
func (c *Catalog) ProductsWithPrefix(prefix string) []*Product {
	var out []*Product
	for i := range c.Products {
		if strings.HasPrefix(c.Products[i].Code, prefix) {
			out = append(out, &c.Products[i])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Requirements собирает требования страны к товару: общие требования страны (common),
// затем требования группы товара, затем уточнения для кода. Второе значение false —
// профиль группы не найден: тогда остаются только общие требования (ТЗ §14.8).
func (c *Country) Requirements(group, code string) (Requirements, bool) {
	g, ok := c.Groups[group]
	p := c.productOverride(code)
	r := Requirements{
		Packaging: concat(c.Common.Packaging, g.Packaging, p.Packaging),
		Product:   concat(c.Common.Product, g.Product, p.Product),
		Labeling:  concat(c.Common.Labeling, g.Labeling, p.Labeling),
		Documents: mergeDocs(mergeDocs(c.Common.Documents, g.Documents), p.Documents),
		Lab:       concat(c.Common.Lab, g.Lab, p.Lab),
		LabDays:   c.Common.LabDays,
		Market:    g.Market,
		// Регистрация нужна, если её требует страна, группа или конкретный товар.
		Registration: c.Common.Registration || g.Registration || p.Registration,
	}
	if g.LabDays[1] > 0 {
		r.LabDays = g.LabDays
	}
	if p.LabDays[1] > 0 {
		r.LabDays = p.LabDays
	}
	if p.Market != "" {
		r.Market = p.Market
	}
	return r, ok
}

// productOverride — уточнение для кода: запись Products с самым длинным подходящим ключом.
func (c *Country) productOverride(code string) Requirements {
	best := ""
	for prefix := range c.Products {
		if code != "" && strings.HasPrefix(code, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	return c.Products[best]
}

func concat(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// mergeDocs объединяет документы: если группа указывает документ с тем же названием,
// что и common, берётся групповая версия (например, «ветсертификат — не требуется»).
func mergeDocs(common, group []Doc) []Doc {
	out := make([]Doc, 0, len(common)+len(group))
	for _, d := range common {
		if !hasDoc(group, d.Name) {
			out = append(out, d)
		}
	}
	return append(out, group...)
}

func hasDoc(docs []Doc, name string) bool {
	for _, d := range docs {
		if strings.EqualFold(d.Name, name) {
			return true
		}
	}
	return false
}
