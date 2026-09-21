// Package data — учебные справочники бота: коды ТН ВЭД, профили стран, меры регулирования.
//
// Сами данные лежат рядом в JSON-файлах и вшиваются в программу при сборке (см. data.go):
//
//	tnved.json          — справочник кодов ТН ВЭД (фрагмент)
//	measures.json       — экспортные пошлины РФ, квоты, запреты
//	countries/cn.json   — требования Китая
//	countries/am.json   — требования Армении
//	countries/kz.json   — требования Казахстана
//
// Чтобы поменять требование или ставку, достаточно отредактировать JSON — код трогать не нужно.
// После правки запустите `go test ./data/` — тест проверит, что справочники согласованы.
//
// ВСЕ данные учебные (ТЗ §17, §24): они приближены к реальности на сентябрь 2026 года,
// но не являются официальной консультацией.
package data

// ---------------------------------------------------------------------------
// Справочник ТН ВЭД (tnved.json)
// ---------------------------------------------------------------------------

// Origin — происхождение продукции. От него зависит вид контроля (ТЗ §22, сценарии 2 и 3):
// животное → ветеринарный сертификат, растительное → фитосанитарный.
type Origin string

const (
	OriginPlant  Origin = "plant"  // растительное: мука, зерно, сахар, масло
	OriginAnimal Origin = "animal" // животное: мёд, мясо, молоко, рыба
	OriginMixed  Origin = "mixed"  // составное: шоколад, печенье
	OriginNone   Origin = "none"   // не пищевое (ноутбуки и т. п.)
)

// Product — одна запись справочника ТН ВЭД.
type Product struct {
	Code     string   `json:"code"`     // 10 цифр без пробелов: "1701121000"
	Name     string   `json:"name"`     // наименование для отчёта: "Сахар-песок свекловичный"
	Keywords []string `json:"keywords"` // слова, по которым пользователь ищет товар
	Food     bool     `json:"food"`     // пищевая продукция? Бот работает только с ней (ТЗ §12)
	Category string   `json:"category"` // категория по справочнику: "Группа 17. Сахар и кондитерские изделия из сахара"
	Group    string   `json:"group"`    // группа требований в countries/*.json: "sugar", "honey", "flour"...
	Origin   Origin   `json:"origin"`

	Unit UnitForms `json:"unit"` // единица по умолчанию: мешок / мешка / мешков

	// UnitKg — типичный вес одной единицы (мешок, коробка, бочка), кг: [мин, макс].
	// Нужен для проверки «количество и вес не соответствуют друг другу» (ТЗ §14.4).
	UnitKg [2]float64 `json:"unit_kg"`

	// TarePct — типичная доля тары в весе брутто, %: [мин, макс] (ТЗ §14.4, брутто/нетто).
	TarePct [2]float64 `json:"tare_pct"`

	// PriceRubPerT — учебная индикативная цена, ₽ за тонну. Используется для адвалорной
	// пошлины вместо стоимости контракта: запрашивать её у пользователя нельзя (ТЗ §17).
	PriceRubPerT float64 `json:"price_rub_per_t"`

	TempC      *TempRange   `json:"temp_c,omitempty"`      // температурный режим перевозки (ТЗ §14.6)
	ReplacedBy *Replacement `json:"replaced_by,omitempty"` // код устарел или будет заменён (ТЗ §14.1)

	// Alternatives — другие коды, по которым товар может классифицироваться (ТЗ §14.7).
	Alternatives []string `json:"alternatives,omitempty"`
	AltNote      string   `json:"alt_note,omitempty"` // чем отличаются варианты классификации

	Components []Component `json:"components,omitempty"` // ингредиенты с особыми требованиями (ТЗ §14.7)

	// Note — пометка для отчёта. Например, у кодов из ТЗ, которых нет в действующей
	// ТН ВЭД, здесь указан реальный код.
	Note     string `json:"note,omitempty"`
	Training bool   `json:"training,omitempty"` // запись учебная (код из ТЗ или условный пример)
}

// UnitForms — формы слова для единицы товара: 1 мешок, 2 мешка, 5 мешков.
type UnitForms struct {
	One  string `json:"one"`
	Few  string `json:"few"`
	Many string `json:"many"`
}

// TempRange — температурный режим перевозки, °C.
type TempRange struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Note string  `json:"note,omitempty"`
}

// Replacement — сведения о замене кода ТН ВЭД.
type Replacement struct {
	Code  string `json:"code"`  // новый код, 10 цифр
	Since string `json:"since"` // дата замены, "2006-01-02"
	Basis string `json:"basis"` // основание (решение ЕЭК)
}

// Component — ингредиент, на который в стране назначения есть отдельные ограничения.
type Component struct {
	Name      string   `json:"name"`      // "мак пищевой"
	Countries []string `json:"countries"` // где действует ограничение: ["cn"]
	Note      string   `json:"note"`      // суть ограничения
}

// ---------------------------------------------------------------------------
// Профиль страны (countries/*.json)
// ---------------------------------------------------------------------------

// Country — профиль страны назначения.
type Country struct {
	ID       string   `json:"id"`        // "cn", "am", "kz"
	Name     string   `json:"name"`      // "Китай"
	FullName string   `json:"full_name"` // "Китайская Народная Республика (КНР)"
	Flag     string   `json:"flag"`      // "🇨🇳"
	To       string   `json:"to"`        // «в Китай» — для фраз «Экспорт в Китай»
	EAEU     bool     `json:"eaeu"`      // член ЕАЭС → пошлин нет (ТЗ §13, сценарий 1)
	Subtitle string   `json:"subtitle"`  // пояснение к кнопке выбора страны (экран 2)
	Aliases  []string `json:"aliases"`   // как ещё можно написать страну: "кнр", "china"

	Registry Registry `json:"registry"` // регистрация производителя в стране назначения (ТЗ §14.3)

	// EAEUCertsRecognized — признаются ли декларации/сертификаты ЕАЭС (ТЗ §14.3).
	EAEUCertsRecognized bool `json:"eaeu_certs_recognized"`

	Authority string `json:"authority"` // где проверить актуальность требований (ТЗ §20.3)

	Common Requirements            `json:"common"` // требования для всех групп товаров
	Groups map[string]Requirements `json:"groups"` // дополнительные требования по группам: "sugar" → ...

	Tax       TaxInfo  `json:"tax"`       // НДС, декларация, сборы
	Logistics []string `json:"logistics"` // особенности маршрута (транзит и т. п.)
	Roles     Roles    `json:"roles"`     // подсказки для 5 ролей (ТЗ §6, §21)
}

// Registry — реестр производителей страны назначения.
type Registry struct {
	Name   string `json:"name"`   // "GACC (система CIFER)"
	Who    string `json:"who"`    // кто подаёт заявку: "Россельхознадзор" / "производитель сам"
	Months string `json:"months"` // ориентировочный срок: "2–6"
}

// Requirements — требования по блокам отчёта (ТЗ §15, экран 5, блоки 1–5).
// Итоговые требования = common страны + группа товара (сначала общие, потом групповые).
type Requirements struct {
	Packaging []string `json:"packaging,omitempty"` // ✅ Упаковка
	Product   []string `json:"product,omitempty"`   // ✅ Продукция
	Labeling  []string `json:"labeling,omitempty"`  // ✅ Маркировка
	Documents []Doc    `json:"documents,omitempty"` // 📋 Сертификаты и разрешения
	Lab       []string `json:"lab,omitempty"`       // 🔬 Лабораторные испытания
	LabDays   [2]int   `json:"lab_days,omitempty"`  // срок испытаний, рабочих дней: [мин, макс]

	// Registration — нужна ли регистрация производителя в реестре страны (ТЗ §14.3).
	Registration bool `json:"registration,omitempty"`

	// Market — барьеры входа для директора ВЭД: ввозные пошлины, квоты, НДС страны.
	Market string `json:"market,omitempty"`
}

// DocStatus — нужен ли документ.
type DocStatus string

const (
	DocRequired    DocStatus = "required"    // обязателен
	DocConditional DocStatus = "conditional" // нужен при условии (см. Note)
	DocNotRequired DocStatus = "no"          // не требуется (показываем явно, как в ТЗ)
)

// Doc — документ из чек-листа (для менеджера ВЭД: что, кто выдаёт, сколько ждать).
type Doc struct {
	Name   string    `json:"name"`           // "Ветеринарный сертификат (форма № 2 ЕАЭС)"
	Status DocStatus `json:"status"`         // required / conditional / no
	Who    string    `json:"who,omitempty"`  // кто выдаёт
	Term   string    `json:"term,omitempty"` // срок получения: "1–3 дня"
	Note   string    `json:"note,omitempty"` // условие или пояснение
}

// TaxInfo — налоги и декларирование.
type TaxInfo struct {
	VAT         string `json:"vat"`         // НДС при экспорте и как его подтвердить
	Declaration string `json:"declaration"` // «Декларация на товары (ЭК 10)» или «статистическая форма»
	CustomsFee  string `json:"customs_fee"` // таможенный сбор
	DutyNote    string `json:"duty_note"`   // пояснение к пошлине (для ЕАЭС)
}

// Roles — одна строка-подсказка для каждой роли из ТЗ §6.
type Roles struct {
	Director      string `json:"director"`       // директор по ВЭД
	Manager       string `json:"manager"`        // менеджер по ВЭД
	Customs       string `json:"customs"`        // эксперт по таможенному регулированию
	Accountant    string `json:"accountant"`     // главный бухгалтер ВЭД
	ExportControl string `json:"export_control"` // служба экспортного контроля
}

// ---------------------------------------------------------------------------
// Меры регулирования (measures.json)
// ---------------------------------------------------------------------------

// Measures — меры регулирования экспорта.
type Measures struct {
	DataAsOf    string        `json:"data_as_of"` // дата актуальности данных, "2006-01-02"
	Duties      []Duty        `json:"duties"`
	Quotas      []Quota       `json:"quotas"`
	ExportBans  []ExportBan   `json:"export_bans"`
	ImportBans  []ImportBan   `json:"import_bans"`
	Antidumping []Antidumping `json:"antidumping"`
}

// DutyType — вид экспортной пошлины (ТЗ §13, шаг 1).
type DutyType string

const (
	DutyNone      DutyType = "none"      // не установлена (0%)
	DutyAdValorem DutyType = "advalorem" // % от таможенной стоимости
	DutySpecific  DutyType = "specific"  // фиксированная сумма за единицу веса
	DutyCombined  DutyType = "combined"  // большая из адвалорной и специфической
)

// Duty — экспортная пошлина РФ (только при вывозе за пределы ЕАЭС) для кодов,
// начинающихся с Prefix. Если подходят несколько записей, берётся самый длинный префикс.
type Duty struct {
	Prefix    string   `json:"prefix"` // "1001" — все коды товарной позиции 1001
	Type      DutyType `json:"type"`
	Name      string   `json:"name"`                    // "Плавающая экспортная пошлина на пшеницу"
	AdValorem float64  `json:"advalorem_pct,omitempty"` // адвалорная часть, %
	Amount    float64  `json:"amount,omitempty"`        // специфическая часть: сумма...
	Currency  string   `json:"currency,omitempty"`      // ...в валюте "RUB" / "EUR" / "USD"...
	PerKg     float64  `json:"per_kg,omitempty"`        // ...за столько кг (обычно 1000)
	Basis     string   `json:"basis"`                   // основание: постановление Правительства РФ
	ValidFrom string   `json:"valid_from,omitempty"`    // действует с, "2006-01-02"
	ValidTo   string   `json:"valid_to,omitempty"`      // действует по
	Note      string   `json:"note,omitempty"`          // что может скоро измениться
	Training  bool     `json:"training,omitempty"`      // учебная (условная) ставка
}

// Quota — экспортная квота (ТЗ §13 шаг 3, §14.2). Действует ежегодно в период From–To
// (формат "01-02": месяц-день).
type Quota struct {
	Prefixes []string `json:"prefixes"`
	Name     string   `json:"name"`
	Tonnes   float64  `json:"tonnes"` // объём квоты
	From     string   `json:"from"`   // начало периода, "MM-DD"
	To       string   `json:"to"`     // конец периода, "MM-DD"
	Year     int      `json:"year"`   // год, для которого известен объём
	Basis    string   `json:"basis"`
	OverNote string   `json:"over_note"` // что будет сверх квоты
	Training bool     `json:"training,omitempty"`
}

// ExportBan — временный запрет вывоза из РФ (ТЗ §14.2).
type ExportBan struct {
	Prefixes  []string `json:"prefixes"`
	Name      string   `json:"name"`
	From      string   `json:"from"` // "2006-01-02"
	To        string   `json:"to"`
	Basis     string   `json:"basis"`
	Countries []string `json:"countries,omitempty"` // на какие страны действует; пусто = за пределы ЕАЭС
	Training  bool     `json:"training,omitempty"`
}

// ImportBan — страна назначения приостановила ввоз из РФ (ТЗ §14.2).
type ImportBan struct {
	Country  string   `json:"country"`
	Prefixes []string `json:"prefixes"`
	Name     string   `json:"name"`
	From     string   `json:"from"`
	To       string   `json:"to,omitempty"`
	Basis    string   `json:"basis"`
	Training bool     `json:"training,omitempty"`
}

// Antidumping — антидемпинговая пошлина страны назначения (ТЗ §13, шаг 3).
// Платит импортёр в стране назначения, поэтому показывается справочно.
type Antidumping struct {
	Country  string   `json:"country"`
	Prefixes []string `json:"prefixes"`
	Pct      float64  `json:"pct"`
	Basis    string   `json:"basis"`
	Training bool     `json:"training,omitempty"`
}
