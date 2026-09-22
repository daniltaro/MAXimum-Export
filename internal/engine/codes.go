// Package engine — расчётное ядро бота: поиск кода ТН ВЭД, разбор и проверка ввода,
// расчёт пошлины, предупреждения (ТЗ §12–§14). Пакет ничего не знает о мессенджере:
// его вызывают экраны бота (internal/flow) и тесты.
package engine

import (
	"strings"
	"time"
	"unicode"

	"maxexport/data"
)

// Engine — ядро с загруженными справочниками. Создаётся один раз при старте программы.
type Engine struct {
	Cat *data.Catalog

	// validFrom — коды, которые начнут действовать только с даты замены старого кода
	// (ТЗ §14.1). До этой даты они не показываются в поиске.
	validFrom map[string]time.Time
}

// New создаёт ядро.
func New(cat *data.Catalog) *Engine {
	e := &Engine{Cat: cat, validFrom: map[string]time.Time{}}
	for _, p := range cat.Products {
		if p.ReplacedBy != nil {
			if since, ok := ParseISODate(p.ReplacedBy.Since); ok {
				e.validFrom[p.ReplacedBy.Code] = since
			}
		}
	}
	return e
}

// currentFor — действующий код, который будет заменён кодом p.
func (e *Engine) currentFor(p *data.Product) *data.Product {
	for i := range e.Cat.Products {
		if r := e.Cat.Products[i].ReplacedBy; r != nil && r.Code == p.Code {
			return &e.Cat.Products[i]
		}
	}
	return nil
}

// ValidFrom — с какой даты действует код (нулевое время — действует уже сейчас).
func (e *Engine) ValidFrom(code string) time.Time { return e.validFrom[code] }

// notYetValid — код ещё не вступил в силу на дату day.
func (e *Engine) notYetValid(p *data.Product, day time.Time) bool {
	since, ok := e.validFrom[p.Code]
	return ok && Day(day).Before(since)
}

// ---------------------------------------------------------------------------
// Коды ТН ВЭД
// ---------------------------------------------------------------------------

// CodeDigits оставляет в строке только цифры: "1701 12 100 0" → "1701121000".
func CodeDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LooksLikeCode — похож ли ввод на код ТН ВЭД (не меньше 4 цифр и почти нет букв).
func LooksLikeCode(s string) bool {
	digits, letters := 0, 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		}
	}
	return digits >= 4 && letters <= 3 // «код 1701...» тоже считаем кодом
}

// FormatCode печатает код в принятом виде: "1701121000" → "1701 12 100 0".
func FormatCode(code string) string {
	switch len(code) {
	case 10:
		return code[:4] + " " + code[4:6] + " " + code[6:9] + " " + code[9:]
	case 8:
		return code[:4] + " " + code[4:6] + " " + code[6:]
	case 6:
		return code[:4] + " " + code[4:]
	default:
		return code
	}
}

// CodeStatus — результат проверки кода, введённого вручную (ТЗ §15, экран 3).
type CodeStatus int

const (
	CodeOK          CodeStatus = iota // код найден, пищевой
	CodeBadFormat                     // не 4–10 цифр
	CodeNotFound                      // нет в справочнике
	CodeNonFood                       // найден, но это не пищевая продукция
	CodeReplaced                      // устарел, заменён новым (расчёт — по новому коду)
	CodePrefix                        // введено 4–9 цифр: показать подходящие 10-значные коды
	CodeNotYetValid                   // код начнёт действовать позже (после замены старого кода)
)

// CodeCheck — подробности проверки кода.
type CodeCheck struct {
	Status     CodeStatus
	Digits     string          // введённые цифры
	Product    *data.Product   // итоговый товар (для CodeReplaced — новый код)
	Old        *data.Product   // устаревший код (только для CodeReplaced)
	Candidates []*data.Product // коды с таким началом (только для CodePrefix)
	Current    *data.Product   // действующий сейчас код (только для CodeNotYetValid)
	ValidFrom  time.Time       // с какой даты начнёт действовать код (только для CodeNotYetValid)
}

// CheckCode проверяет код, введённый пользователем (ТЗ §12: «проверяет код по справочнику,
// подтверждает, что код существует и соответствует категории "пищевая продукция"»).
func (e *Engine) CheckCode(input string, today time.Time) CodeCheck {
	d := CodeDigits(input)
	res := CodeCheck{Digits: d}
	switch {
	case len(d) == 10:
		p := e.Cat.Product(d)
		switch {
		case p == nil:
			res.Status = CodeNotFound
		case !p.Food:
			res.Status, res.Product = CodeNonFood, p
		case e.isReplacedOn(p, today):
			res.Status, res.Old, res.Product = CodeReplaced, p, e.Cat.Product(p.ReplacedBy.Code)
		case e.notYetValid(p, today):
			res.Status, res.Product, res.Current, res.ValidFrom = CodeNotYetValid, p, e.currentFor(p), e.validFrom[p.Code]
		default:
			res.Status, res.Product = CodeOK, p
		}
	case len(d) >= 4 && len(d) < 10:
		for _, p := range e.Cat.ProductsWithPrefix(d) {
			if !e.isReplacedOn(p, today) {
				res.Candidates = append(res.Candidates, p)
			}
		}
		if len(res.Candidates) == 0 {
			res.Status = CodeNotFound
		} else {
			res.Status = CodePrefix
		}
	default:
		res.Status = CodeBadFormat
	}
	return res
}

// isReplacedOn — код уже заменён на дату day (и новый код есть в справочнике).
func (e *Engine) isReplacedOn(p *data.Product, day time.Time) bool {
	if p.ReplacedBy == nil || e.Cat.Product(p.ReplacedBy.Code) == nil {
		return false
	}
	since, ok := ParseISODate(p.ReplacedBy.Since)
	return ok && !Day(day).Before(since)
}
