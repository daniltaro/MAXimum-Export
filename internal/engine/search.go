package engine

import (
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"maxexport/data"
)

// ---------------------------------------------------------------------------
// Нормализация текста и нечёткое сравнение слов
// ---------------------------------------------------------------------------

// Normalize приводит текст к виду для поиска: строчные буквы, «ё» → «е»,
// знаки препинания и дефисы → пробелы. "Сахар-Песок!" → "сахар песок".
func Normalize(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if r == 'ё' {
			r = 'е'
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space && b.Len() > 0 {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// stopWords — служебные слова, которые не участвуют в поиске.
var stopWords = map[string]bool{"и": true, "с": true, "в": true, "из": true, "для": true, "по": true, "на": true, "без": true, "или": true}

// Words разбивает текст на значимые слова для поиска.
func Words(s string) []string {
	var out []string
	for _, w := range strings.Fields(Normalize(s)) {
		if !stopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

// Уровни совпадения двух слов (больше — лучше).
const (
	matchNone   = 0
	matchTypo   = 1 // опечатка: "пшениичная" ≈ "пшеничная"
	matchPrefix = 2 // другое окончание: "муки" ≈ "мука", "сахарный" ≈ "сахар"
	matchExact  = 3
)

// wordMatch сравнивает слово запроса q со словом справочника w.
func wordMatch(q, w string) int {
	if q == w {
		return matchExact
	}
	lq, lw := utf8.RuneCountInString(q), utf8.RuneCountInString(w)
	short, long := min(lq, lw), max(lq, lw)

	// Общее начало слова: "сахар" — "сахарный", "мука" — "муки", "мак" — "маком".
	// Требуем, чтобы общая часть была не короче 3 букв, отличалась от короткого слова
	// не больше чем на 2 буквы (окончание) и покрывала ≥ 60% длинного слова —
	// иначе "молоко" совпадёт с "молотый".
	cp := commonPrefix(q, w)
	if cp >= 3 && cp >= short-2 && cp*5 >= long*3 {
		return matchPrefix
	}

	// Опечатки: 1 для слов от 4 букв, 2 — для длинных слов от 8 букв.
	if short >= 4 {
		allowed := 1
		if short >= 8 {
			allowed = 2
		}
		if damerauLevenshtein(q, w) <= allowed {
			return matchTypo
		}
	}
	return matchNone
}

// WordsMatch — совпадают ли два слова с учётом окончаний и опечаток
// (используется и в ответах на вопросы: «сертификаты» ≈ «сертификат»).
func WordsMatch(a, b string) bool { return wordMatch(a, b) != matchNone }

func commonPrefix(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return n
}

// damerauLevenshtein — число правок (вставка, удаление, замена, перестановка соседних букв),
// чтобы получить из a слово b.
func damerauLevenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[la][lb]
}

// ---------------------------------------------------------------------------
// Поиск товара по названию (ТЗ §15, экран 3)
// ---------------------------------------------------------------------------

// Hit — найденный код ТН ВЭД и насколько хорошо он совпал с запросом.
type Hit struct {
	Product *data.Product
	Score   int
}

// SearchProducts ищет пищевые товары по словам запроса. Товар подходит, если КАЖДОЕ слово
// запроса нашлось в названии или ключевых словах (с учётом окончаний и опечаток).
// Результат отсортирован: сначала лучшие совпадения. Коды, устаревшие на дату today,
// не показываются.
func (e *Engine) SearchProducts(query string, today time.Time) []Hit {
	qWords := Words(query)
	if len(qWords) == 0 {
		return nil
	}
	qPhrase := strings.Join(qWords, " ")

	var hits []Hit
	for i := range e.Cat.Products {
		p := &e.Cat.Products[i]
		if !p.Food || e.isReplacedOn(p, today) || e.notYetValid(p, today) {
			continue
		}
		score, ok := scoreProduct(p, qWords, qPhrase)
		if ok {
			hits = append(hits, Hit{Product: p, Score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Product.Code < hits[j].Product.Code
	})
	return hits
}

func scoreProduct(p *data.Product, qWords []string, qPhrase string) (int, bool) {
	phrases := append([]string{p.Name}, p.Keywords...)
	var words []string
	for _, ph := range phrases {
		words = append(words, Words(ph)...)
	}

	score := 0
	for _, q := range qWords {
		best := matchNone
		for _, w := range words {
			if m := wordMatch(q, w); m > best {
				best = m
			}
		}
		if best == matchNone {
			return 0, false // одно из слов запроса не нашлось — товар не подходит
		}
		score += best
	}

	// Бонусы за совпадение фразы целиком: запрос «сахар-песок» точнее всего
	// совпадает с товаром, у которого ровно такое ключевое слово.
	for i, ph := range phrases {
		n := strings.Join(Words(ph), " ")
		switch {
		case n == qPhrase && i == 0:
			score += 12 // совпало наименование
		case n == qPhrase:
			score += 10 // совпало ключевое слово
		case strings.HasPrefix(n, qPhrase):
			score += 4
		}
	}
	// Небольшой приоритет кодам из ТЗ: они используются в сценариях тренинга.
	if p.Training && p.ReplacedBy == nil {
		score++
	}
	return score, true
}

// ---------------------------------------------------------------------------
// Распознавание страны, введённой текстом (ТЗ §15, экран 2)
// ---------------------------------------------------------------------------

// CountryMatch — результат распознавания страны.
type CountryMatch struct {
	Country *data.Country
	Exact   bool // написано ровно название («Китай») — подтверждение не нужно
}

// MatchCountry распознаёт страну: точное название, синоним («КНР»), падеж или опечатку
// («Китая», «Казахтан»). Если страна не распознана — Country == nil.
func (e *Engine) MatchCountry(text string) CountryMatch {
	n := Normalize(text)
	if n == "" {
		return CountryMatch{}
	}
	for i := range e.Cat.Countries {
		c := &e.Cat.Countries[i]
		if n == Normalize(c.Name) {
			return CountryMatch{Country: c, Exact: true}
		}
	}
	for i := range e.Cat.Countries {
		c := &e.Cat.Countries[i]
		for _, a := range c.Aliases {
			if n == Normalize(a) {
				return CountryMatch{Country: c}
			}
		}
	}
	// Падежи и опечатки проверяем только по названию страны и однословным синонимам.
	for i := range e.Cat.Countries {
		c := &e.Cat.Countries[i]
		for _, w := range strings.Fields(n) {
			if wordMatch(w, Normalize(c.Name)) >= matchTypo {
				return CountryMatch{Country: c}
			}
		}
	}
	return CountryMatch{}
}
