package engine

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// MSK — московское время. Все даты в отчётах показываются по Москве, где бы ни работал сервер.
// (В Москве нет перехода на летнее время, поэтому достаточно фиксированного сдвига +3 часа.)
var MSK = time.FixedZone("MSK", 3*60*60)

// FormatNum печатает число по-русски: пробел между тысячами, запятая перед дробной частью,
// без лишних нулей: 250000 → "250 000", 725.7 → "725,7". После запятой — до 2 знаков,
// а у чисел меньше 1 — до 3 знаков (вес единицы 0,005 кг из сценария 3 ТЗ).
func FormatNum(v float64) string {
	neg := v < 0
	v = math.Abs(v)
	decimals := 2
	if v < 1 {
		decimals = 3
	}
	scale := math.Pow10(decimals)
	v = math.Round(v*scale) / scale
	whole := math.Floor(v)
	frac := int64(math.Round((v - whole) * scale))

	s := groupThousands(strconv.FormatFloat(whole, 'f', 0, 64))
	if frac > 0 {
		f := strconv.FormatInt(frac, 10)
		f = strings.Repeat("0", decimals-len(f)) + f
		s += "," + strings.TrimRight(f, "0")
	}
	if neg {
		s = "−" + s
	}
	return s
}

// FormatInt печатает целое число с пробелами между тысячами: 5000 → "5 000".
func FormatInt(n int64) string {
	if n < 0 {
		return "−" + groupThousands(strconv.FormatInt(-n, 10))
	}
	return groupThousands(strconv.FormatInt(n, 10))
}

// FormatRub печатает сумму в рублях: 625000 → "625 000 ₽".
func FormatRub(v float64) string { return FormatNum(v) + " ₽" }

// FormatKg печатает вес: 250000 → "250 000 кг (250 т)"; маленькие веса — без тонн.
func FormatKg(kg float64) string {
	if kg >= 1000 {
		return FormatNum(kg) + " кг (" + FormatNum(kg/1000) + " т)"
	}
	return FormatNum(kg) + " кг"
}

func groupThousands(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	head := len(digits) % 3
	if head > 0 {
		b.WriteString(digits[:head])
	}
	for i := head; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

// Plural выбирает форму слова для числа: Plural(5, "мешок", "мешка", "мешков") → "мешков".
func Plural(n int64, one, few, many string) string {
	n = n % 100
	if n < 0 {
		n = -n
	}
	if n >= 11 && n <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

// FormatDate печатает дату в формате ДД.ММ.ГГГГ по московскому времени.
func FormatDate(t time.Time) string { return t.In(MSK).Format("02.01.2006") }

// ParseISODate разбирает дату из справочников ("2006-01-02") как московскую полночь.
func ParseISODate(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, MSK)
	return t, err == nil
}

// Day обрезает время до начала суток по Москве — чтобы сравнивать даты, а не моменты.
func Day(t time.Time) time.Time {
	y, m, d := t.In(MSK).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, MSK)
}

// BusinessDaysBetween — число рабочих дней (пн–пт) от from (не включая) до to (включая).
// Праздники не учитываются — это ориентир для предупреждения о сроках испытаний.
func BusinessDaysBetween(from, to time.Time) int {
	from, to = Day(from), Day(to)
	n := 0
	for d := from.AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n++
		}
	}
	return n
}
