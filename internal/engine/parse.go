package engine

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Разбор чисел, количества, веса и дат из текста пользователя (ТЗ §15, экран 4)
// ---------------------------------------------------------------------------

// numberRe находит первое число в тексте: "-5", "250 000", "250000,5", "0.25".
// Пробелы внутри числа допускаются только группами по 3 цифры ("5 000").
var numberRe = regexp.MustCompile(`[-−]?\d{1,3}(?:[  ]\d{3})+(?:[.,]\d+)?|[-−]?\d+(?:[.,]\d+)?`)

// ParseNumber находит в тексте первое число. ok=false — чисел нет.
func ParseNumber(s string) (v float64, rest string, ok bool) {
	loc := numberRe.FindStringIndex(s)
	if loc == nil {
		return 0, s, false
	}
	raw := s[loc[0]:loc[1]]
	raw = strings.NewReplacer(" ", "", " ", "", "−", "-", ",", ".").Replace(raw)
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, s, false
	}
	return v, s[loc[1]:], true
}

// Ошибки ввода: тексты — из ТЗ §14.9 и §15 (экран 4).
var (
	ErrNoNumber     = errors.New("⚠️ Не вижу числа. Введите число, например: 5000")
	ErrQtyNonPos    = errors.New("⚠️ Количество должно быть больше нуля.")
	ErrQtyNotInt    = errors.New("⚠️ Количество — целое число единиц (мешков, коробок). Например: 5000")
	ErrWeightNonPos = errors.New("⚠️ Вес должен быть больше нуля. Проверьте ввод.")
	ErrNetGtGross   = errors.New("⚠️ Вес нетто не может быть больше веса брутто. Проверьте ввод.")
)

// Quantity — разобранное количество: "5000 мешков (по 50 кг)".
type Quantity struct {
	N         int64
	UnitWord  string  // слово единицы, если пользователь его написал: "мешков"
	PerUnitKg float64 // «по 50 кг», если указано — подсказка для ввода веса
}

// Внимание: в Go `\b` и `\w` понимают только латиницу, поэтому для русских слов
// границу слова пишем явно: (?:[^\p{L}]|$) — «дальше не буква или конец строки».
var perUnitRe = regexp.MustCompile(`по\s+(\d+(?:[.,]\d+)?)\s*(кг|килограмм\p{L}*|т|тонн\p{L}*)(?:[^\p{L}]|$)`)
var unitWordRe = regexp.MustCompile(`^\s*(\p{L}+)`)

// ParseQuantity разбирает количество единиц товара.
func ParseQuantity(s string) (Quantity, error) {
	v, rest, ok := ParseNumber(s)
	if !ok {
		return Quantity{}, ErrNoNumber
	}
	if v <= 0 {
		return Quantity{}, ErrQtyNonPos
	}
	if v != float64(int64(v)) {
		return Quantity{}, ErrQtyNotInt
	}
	q := Quantity{N: int64(v)}
	if m := unitWordRe.FindStringSubmatch(rest); m != nil {
		w := strings.ToLower(m[1])
		if w != "по" && w != "шт" { // «шт» — это и так «единицы»
			q.UnitWord = w
		}
	}
	if m := perUnitRe.FindStringSubmatch(strings.ToLower(s)); m != nil {
		n, _, _ := ParseNumber(m[1])
		if strings.HasPrefix(m[2], "т") {
			n *= 1000
		}
		q.PerUnitKg = n
	}
	return q, nil
}

// Weight — разобранный вес партии.
type Weight struct {
	Kg       float64 // вес брутто в килограммах
	Explicit bool    // единица указана явно («кг» или «т») — переспрашивать не нужно
	NetKg    float64 // вес нетто, если указан через «/»: "250000 / 248000"
}

var tonnesRe = regexp.MustCompile(`^\s*(т|тн|тонн\p{L}*|tons?|t)(?:[^\p{L}]|$)`)
var kgRe = regexp.MustCompile(`^\s*(кг|килограмм\p{L}*|kg)(?:[^\p{L}]|$)`)

// ParseWeight разбирает вес: "250000", "250 000 кг", "250 т", "250,5 тонн",
// "250000 / 248000" (брутто / нетто).
func ParseWeight(s string) (Weight, error) {
	s = strings.ToLower(s)
	gross, net := s, ""
	if i := strings.Index(s, "/"); i >= 0 {
		gross, net = s[:i], s[i+1:]
	}

	w, err := parseOneWeight(gross)
	if err != nil {
		return Weight{}, err
	}
	if strings.TrimSpace(net) != "" {
		n, err := parseOneWeight(net)
		if err != nil {
			return Weight{}, err
		}
		if !n.Explicit && w.Explicit && !kgRe.MatchString(net) {
			// "250 т / 248" — единица нетто такая же, как у брутто
			n.Kg = n.Kg * (w.Kg / w.rawValue)
		}
		if n.Kg > w.Kg {
			return Weight{}, ErrNetGtGross
		}
		w.NetKg = n.Kg
	}
	return w.Weight, nil
}

type parsedWeight struct {
	Weight
	rawValue float64
}

func parseOneWeight(s string) (parsedWeight, error) {
	v, rest, ok := ParseNumber(s)
	if !ok {
		return parsedWeight{}, ErrNoNumber
	}
	if v <= 0 {
		return parsedWeight{}, ErrWeightNonPos
	}
	w := parsedWeight{Weight: Weight{Kg: v}, rawValue: v}
	switch {
	case tonnesRe.MatchString(rest):
		w.Kg, w.Explicit = v*1000, true
	case kgRe.MatchString(rest):
		w.Explicit = true
	}
	return w, nil
}

// ParseDate разбирает дату отгрузки: "15.11.2026", "15.11" (ближайшая такая дата), "15/11/26".
func ParseDate(s string, today time.Time) (time.Time, bool) {
	s = strings.TrimSpace(strings.NewReplacer("/", ".", "-", ".").Replace(s))
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return time.Time{}, false
	}
	d, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return time.Time{}, false
	}
	today = Day(today)
	y := today.Year()
	if len(parts) == 3 {
		yy, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil {
			return time.Time{}, false
		}
		if yy < 100 {
			yy += 2000
		}
		y = yy
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, MSK)
	if t.Day() != d || int(t.Month()) != m { // 31.02 и т. п.
		return time.Time{}, false
	}
	if len(parts) == 2 && t.Before(today) {
		t = t.AddDate(1, 0, 0)
	}
	return t, true
}

// ---------------------------------------------------------------------------
// Защита от персональных данных (ТЗ §17, §21: «нет запроса персональных данных»)
// ---------------------------------------------------------------------------

var piiPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"e-mail", regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`)},
	{"номер телефона", regexp.MustCompile(`(?:\+7|\b8)[\s(-]*\d{3}[\s)-]*\d{3}[\s-]*\d{2}[\s-]*\d{2}\b`)},
	{"ИНН/ОГРН/КПП", regexp.MustCompile(`(?i)(?:^|[^\p{L}])(инн|огрн|огрнип|кпп)(?:[^\p{L}]|$)`)},
	{"номер счёта", regexp.MustCompile(`\b\d{20}\b`)},
	{"номер договора или инвойса", regexp.MustCompile(`(?i)(договор|контракт|инвойс|invoice|сч[её]т-фактур)\p{L}*\s*(№|#|номер)\s*\S+`)},
	{"номер декларации", regexp.MustCompile(`\b\d{8}/\d{6}/\d{7}\b`)},
}

// DetectPII возвращает вид персональных/закрытых данных в тексте или "" если их нет.
// Такой текст бот не сохраняет и просит не присылать (ТЗ §17).
func DetectPII(s string) string {
	for _, p := range piiPatterns {
		if p.re.MatchString(s) {
			return p.kind
		}
	}
	return ""
}
