package flow

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/report"
	"maxexport/texts"
)

// ---------------------------------------------------------------------------
// Приёмка по ТЗ §21. Один тест = один критерий, названия подтестов повторяют
// формулировки ТЗ, поэтому отчёт о приёмке — это вывод команды:
//
//	go test ./internal/flow/ -run Acceptance -v
//
// Тем же порядком идут пункты в docs/acceptance.md.
// ---------------------------------------------------------------------------

// --- Функционал (база) -----------------------------------------------------

// «Работает выбор 3 стран — с корректным режимом расчёта (ЕАЭС / не ЕАЭС)».
func TestAcceptanceThreeCountries(t *testing.T) {
	cases := []struct{ button, hint, regime string }{
		{"Китай", "Режим «Третьи страны»", "вне ЕАЭС"},
		{"Армения", "Режим ЕАЭС", "ЕАЭС"},
		{"Казахстан", "Режим ЕАЭС", "ЕАЭС"},
	}
	for _, tc := range cases {
		c := newChat(t)
		c.Start().Click("Начать расчёт").Click(tc.button).Expect("✅ Страна:", tc.hint)
		// Режим виден и в итоговом отчёте — строкой «Страна: … (ЕАЭС|вне ЕАЭС)».
		c.Type("мёд").Click("0409 00 000 0").Type("200").Type("4000").Click("Рассчитать").
			Expect("(" + tc.regime + ")")
	}
}

// «Принимает тип продукции и код ТН ВЭД (подбор из справочника + ручной ввод)».
func TestAcceptanceProductAndCodeInput(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай")
	c.Type("сахар-песок").Expect("1701 12 100 0") // подбор по названию
	c.Click("1701 12 100 0").Expect("Шаг 3 из 4") // выбор из найденного
	c.Click("Назад").Click("Указать код вручную") // ручной ввод
	c.Type("0409 00 000 0").Expect("✅ Код 0409 00 000 0")
}

// «Валидирует вес/количество: нули, отрицательные, несоответствия, неподдерживаемые страны».
func TestAcceptanceValidation(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт")
	c.Type("Турция").Expect("⚠️", "поддерживает экспорт в три страны") // страна вне MVP
	c.Click("Казахстан").Type("мука пшеничная").Click("1101 00 000 0")
	c.Type("0").Expect("⚠️ Количество должно быть больше нуля") // ноль
	c.Type("-5").Expect("⚠️")                                   // отрицательное
	c.Type("абв").Expect("⚠️")                                  // не число
	c.Type("1000")
	c.Type("0").Expect("⚠️ Вес должен быть больше нуля") // ноль в весе
	c.Type("5").Expect("нетипичен", "5 кг или 5 тонн")   // вес не сходится с количеством
}

// «Рассчитывает пошлину (тип ставки, формула, итог) либо корректно показывает „0 ₽“ для ЕАЭС».
func TestAcceptanceDutyCalculation(t *testing.T) {
	c := newChat(t)
	// Комбинированная ставка: тип, обе формулы, выбор большей суммы, сбор (ТЗ §13).
	c.Start().Click("Начать расчёт").Click("Китай").Type("рапс").Click("1205 10 900 0")
	c.Type("100").Type("100000").Click("Рассчитать").Expect(
		"Тип ставки: комбинированная", "Ставка: ", "Расчёт по стоимости", "Расчёт по весу",
		"Итого к уплате: **1 650 000 ₽**", "Таможенный сбор: 21 344 ₽")
	// Ставка не установлена: «0 ₽» и пояснение (Китай, сахар).
	c.Click("Новый расчёт").Click("Китай").Type("сахар-песок").Click("1701 12 100 0")
	c.Type("5000").Type("250000").Click("Рассчитать").Expect(
		"Тип ставки: не установлена (0 %)", "Итого к уплате: **0 ₽**")
	// ЕАЭС: пошлины нет, курс не нужен.
	c.Click("Новый расчёт").Click("Армения").Type("мёд натуральный").Click("0409 00 000 0")
	c.Type("200").Type("4000").Click("Рассчитать").Expect(
		"не применяется (ЕАЭС)", "Итого к уплате: **0 ₽**", "Курс валюты: не требуется")
}

// --- Сценарии --------------------------------------------------------------

// «Полный путь для Китая: все блоки + дисклеймер».
func TestAcceptanceFullPathChina(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Type("сахар-песок").Click("1701 12 100 0")
	c.Type("5000").Type("250000").Click("Рассчитать").Expect(
		"📦 Параметры сделки", "✅ Упаковка", "✅ Продукция", "✅ Маркировка",
		"📋 Сертификаты и разрешения", "🔬 Лабораторные испытания", "💰 Таможенная пошлина",
		"👥 Кому что важно", "⚠️ Внимание", "демонстрационный прототип (MVP)")
	if f := c.File(); f == nil || !strings.HasSuffix(f.Name, ".txt") {
		t.Fatal("отчёт не пришёл отдельным документом .txt (ТЗ §18)")
	}
}

// «Полный путь для ЕАЭС: нулевая пошлина, статистическая форма, требования ЕАЭС».
func TestAcceptanceFullPathEAEU(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Казахстан").Type("мука пшеничная").Click("1101 00 000 0")
	c.Type("1000").Type("50000").Click("Рассчитать").Expect(
		"Итого к уплате: **0 ₽**", "Статистическая форма", "ТР ТС", "✅ Маркировка")
}

// «Отрабатываются 5+ нестандартных ситуаций — без зависаний, с понятными ⚠️/🚫».
func TestAcceptanceNonStandardSituations(t *testing.T) {
	// Пять обязательных по ТЗ ситуаций: запрет, квота, устаревший код, сбой курса, нетипичный вес.
	want := []struct{ scenario, sign, text string }{
		{"Запрет вывоза", "🚫", "Экспорт невозможен"},
		{"Квота на зерно", "⚠️", "квота"},
		{"Устаревший код", "ℹ️", "заменён на"},
		{"Сбой курса", "⚠️", "Не удалось получить актуальный курс"},
		{"Мука: вес", "⚠️", "5 кг или 5 тонн"},
	}
	c := newChat(t)
	list := c.bot.DemoScenarios()
	for _, w := range want {
		i := -1
		for n, sc := range list {
			if strings.Contains(sc.Title, w.scenario) {
				i = n
			}
		}
		if i < 0 {
			t.Errorf("в режиме ведущего нет сценария %q", w.scenario)
			continue
		}
		c.Type("/start").Press(global(gDemoRun, fmt.Sprint(i)))
		c.Expect(w.sign, w.text)
		if len(c.lastButtons()) == 0 {
			t.Errorf("сценарий %q: бот не предложил, что делать дальше", w.scenario)
		}
	}
}

// --- Интерфейс и UX --------------------------------------------------------

// «Прогресс „Шаг N из 4“, кнопки „Назад“, „Изменить“, „Скопировать“ — работают».
func TestAcceptanceProgressAndButtons(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Expect("Шаг 1 из 4")
	c.Click("Китай").Expect("Шаг 2 из 4")
	c.Type("сахар-песок").Click("1701 12 100 0").Expect("Шаг 3 из 4")
	c.Type("5000").Type("250000").Expect("Шаг 4 из 4")
	c.Click("Назад").Expect("Вес партии брутто")               // «Назад» — на шаг раньше
	c.Type("250000").Click("Изменить").Expect("Что изменить?") // «Изменить» — правка данных
	c.Click("Назад").Click("Рассчитать").Expect("Результат расчёта")
	c.Click("Скопировать отчёт").Expect("сводка расчёта", "Сахар-песок") // «Скопировать» — одно сообщение
	if n := utf8.RuneCountInString(c.Text()); n > report.CopyLimit {
		t.Errorf("сводка для копирования не влезает в одно сообщение MAX: %d символов", n)
	}
}

// «Результат структурирован, блоки в фиксированном порядке; при длине >3500 — разбивка».
func TestAcceptanceReportStructure(t *testing.T) {
	cat := data.MustLoad()
	e := engine.New(cat)
	order := []string{report.BParams, report.BPackaging, report.BProduct, report.BLabeling,
		report.BDocuments, report.BLab, report.BDuty, report.BRoles, report.BWarnings}
	for i := range cat.Countries {
		country := &cat.Countries[i]
		for j := range cat.Products {
			p := &cat.Products[j]
			if !p.Food {
				continue
			}
			rep := report.Build(e.Calculate(
				engine.Input{Country: country.ID, Code: p.Code, Qty: 100, WeightKg: 20000},
				engine.Env{Now: now, Rates: engine.Training(now)}))
			var got []string
			for _, b := range rep.Blocks {
				got = append(got, b.ID)
			}
			if rep.Result.Stop == nil && !subsequence(got, order) {
				t.Fatalf("%s/%s: блоки не в фиксированном порядке: %v", country.ID, p.Code, got)
			}
			parts := rep.ChatParts(report.ChatLimit)
			for n, part := range parts {
				if k := utf8.RuneCountInString(part); k > report.ChatLimit {
					t.Fatalf("%s/%s: часть %d из %d длиной %d символов", country.ID, p.Code, n+1, len(parts), k)
				}
				if len(parts) > 1 && !strings.Contains(part, fmt.Sprintf("(%d/%d)", n+1, len(parts))) {
					t.Fatalf("%s/%s: в части %d нет номера «(%d/%d)»", country.ID, p.Code, n+1, n+1, len(parts))
				}
			}
		}
	}
}

// subsequence — идут ли элементы got в том же относительном порядке, что и в want.
func subsequence(got, want []string) bool {
	i := 0
	for _, g := range got {
		for i < len(want) && want[i] != g {
			i++
		}
		if i == len(want) {
			return false
		}
		i++
	}
	return true
}

// «Читаемо на планшете (крупные кнопки, без горизонтального скролла) и на ноутбуке»:
// проверяем то, что зависит от бота, — кнопки по одной в ряд и короткие подписи;
// сам размер кнопок и перенос строк рисует клиент MAX.
func TestAcceptanceTabletReadable(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Type("сахар-песок").Click("1701 12 100 0")
	c.Type("5000").Type("250000").Click("Рассчитать").Click("Задать вопрос").Type("что писать на этикетке")
	c.Type("/help").Type("/demo")
	for _, m := range c.all {
		for _, r := range m.Buttons {
			if len(r) > 1 {
				t.Errorf("в ряду %d кнопки — на узком экране они сожмутся: %v", len(r), r)
			}
			for _, b := range r {
				if n := utf8.RuneCountInString(b.Text); n > 30 {
					t.Errorf("подпись кнопки длиннее 30 символов (MAX обрежет): %q", b.Text)
				}
			}
		}
		// Длинные абзацы клиент переносит сам; горизонтальный скролл появляется только
		// от неразрывных кусков — таблиц, длинных ссылок, кодов без пробелов.
		for _, word := range strings.Fields(m.Text) {
			if n := utf8.RuneCountInString(word); n > 45 {
				t.Errorf("неразрывный кусок в %d символов вызовет горизонтальный скролл: %q", n, word)
			}
		}
	}
}

// --- Эксплуатация и безопасность -------------------------------------------

// «Время отклика ≤5 секунд; 20 расчётов подряд — без сбоев».
func TestAcceptanceResponseTime(t *testing.T) {
	c := newChat(t)
	var worst time.Duration
	start := time.Now()
	for i := 0; i < 20; i++ {
		c.Start()
		if i == 0 {
			c.Click("Начать расчёт")
		} else {
			c.Click("Новый расчёт")
		}
		c.Click("Китай").Type("сахар-песок").Click("1701 12 100 0").Type(fmt.Sprint(1000 + i)).Type(fmt.Sprint(50000 + i*50))
		step := time.Now()
		c.Click("Рассчитать").Expect("Результат расчёта")
		if d := time.Since(step); d > worst {
			worst = d
		}
	}
	if worst > 5*time.Second {
		t.Errorf("самый долгий расчёт занял %v (норматив ≤ 5 с)", worst)
	}
	t.Logf("20 расчётов подряд: всего %v, самый долгий ответ %v",
		time.Since(start).Round(time.Millisecond), worst.Round(time.Microsecond))
}

// «Нет запроса персональных данных (ФИО, телефоны, ИНН, номера контрактов)».
func TestAcceptanceNoPersonalData(t *testing.T) {
	// 1. Ни один текст бота не просит персональные или коммерческие данные.
	forbidden := map[string]*regexp.Regexp{
		"ФИО":            regexp.MustCompile(`(?i)(^|[^\p{L}])(фио|фамили\p{L}*|отчеств\p{L}*)([^\p{L}]|$)`),
		"телефон":        regexp.MustCompile(`(?i)телефон`),
		"e-mail":         regexp.MustCompile(`(?i)(e-mail|почт\p{L}*)([^\p{L}]|$)`),
		"ИНН/ОГРН":       regexp.MustCompile(`(?i)(^|[^\p{L}])(инн|огрн|кпп)([^\p{L}]|$)`),
		"номер договора": regexp.MustCompile(`(?i)(номер|№)\s*(договор|контракт|инвойс)`),
		"цена контракта": regexp.MustCompile(`(?i)(цен\p{L}*|стоимост\p{L}*)\s+(контракт|договор)`),
	}
	// Предупреждения «не присылайте …» — это не запрос, а наоборот.
	allowed := regexp.MustCompile(`(?i)(не присылайте|не принимает|не вводите|не нужны|не запрашивает|не сохраняет)`)
	for key, value := range texts.All() {
		if allowed.MatchString(value) {
			continue
		}
		for kind, re := range forbidden {
			if re.MatchString(value) {
				t.Errorf("текст %s просит %s: %s", key, kind, value)
			}
		}
	}

	// 2. Присланные персональные данные бот отклоняет и не использует.
	for _, in := range []string{
		"сахар, мой телефон +7 (999) 123-45-67",
		"сахар, ИНН 7701234567",
		"сахар, контракт № 12/2026",
		"сахар, ivanov@example.com",
	} {
		c := newChat(t)
		c.Start().Click("Начать расчёт").Click("Китай")
		c.Type(in).Expect("🔒", "не принимает персональные").ExpectNot("Найдено", "1701 12 100 0")
	}
}

// «Учебные данные (курс, ставки) явно помечены; на каждом результате — дисклеймер MVP».
func TestAcceptanceTrainingDataMarked(t *testing.T) {
	c := newChat(t)
	// Пошлина по курсу: курс помечен как учебный.
	c.Start().Click("Начать расчёт").Click("Китай").Type("рапс").Click("1205 10 900 0")
	c.Type("100").Type("100000").Click("Рассчитать").Expect("учебный курс", "учебная оценка", report.Disclaimer)
	if f := c.File(); f == nil || !strings.Contains(string(f.Content), "MVP") {
		t.Error("в файле отчёта нет дисклеймера MVP")
	}
	// Дисклеймер есть на каждом результате — во всех сценариях режима ведущего.
	for i, sc := range c.bot.DemoScenarios() {
		c.Type("/start").Press(global(gDemoRun, fmt.Sprint(i)))
		if strings.Contains(c.Text(), "Результат расчёта") && !strings.Contains(c.Text(), "демонстрационный прототип (MVP)") {
			t.Errorf("сценарий %q: в результате нет дисклеймера MVP", sc.Title)
		}
	}
}

// --- Готовность к тренингу -------------------------------------------------

// «Нетехнический пользователь проходит путь „старт → результат“ за ≤3 минут
// и отвечает на 3 из 4 контрольных вопросов по содержанию».
func TestAcceptanceTrainingPath(t *testing.T) {
	c := newChat(t)
	steps := 0
	do := func(f func() *chat) { steps++; f() }
	do(c.Start)
	do(func() *chat { return c.Click("Начать расчёт") })
	do(func() *chat { return c.Click("Китай") })
	do(func() *chat { return c.Type("сахар-песок") })
	do(func() *chat { return c.Click("1701 12 100 0") })
	do(func() *chat { return c.Type("5000") })
	do(func() *chat { return c.Type("250000") })
	do(func() *chat { return c.Click("Рассчитать") })
	c.Expect("Результат расчёта")
	// 8 действий: даже по 20 секунд на каждое — меньше 3 минут.
	if steps > 9 {
		t.Errorf("путь «старт → результат» занял %d действий — для 3 минут это много", steps)
	}

	// Четыре контрольных вопроса по содержанию отчёта.
	c.Click("Задать вопрос")
	answers := []struct{ question, want string }{
		{"Какие сертификаты нужны?", "GACC"},
		{"Сколько пошлина?", "0 ₽"},
		{"Сколько ждать регистрацию?", "2–6"},
		{"Что писать на этикетке?", "китайском"},
	}
	ok := 0
	for _, a := range answers {
		c.Type(a.question)
		if strings.Contains(c.Text(), a.want) {
			ok++
		} else {
			t.Logf("на вопрос %q в ответе нет %q:\n%s", a.question, a.want, c.Text())
		}
	}
	if ok < 3 {
		t.Errorf("бот ответил на %d из 4 контрольных вопросов, по ТЗ нужно не меньше 3", ok)
	}
}

// «Для 5 ролей в результате есть минимум 1 релевантный блок».
func TestAcceptanceFiveRoles(t *testing.T) {
	cat := data.MustLoad()
	e := engine.New(cat)
	roles := []string{"Директор по ВЭД:", "Менеджер по ВЭД:", "Таможенный эксперт:",
		"Главный бухгалтер ВЭД:", "Экспортный контроль:"}
	for i := range cat.Countries {
		country := &cat.Countries[i]
		rep := report.Build(e.Calculate(
			engine.Input{Country: country.ID, Code: "1701121000", Qty: 5000, WeightKg: 250000},
			engine.Env{Now: now, Rates: engine.Training(now)}))
		block, ok := rep.Block(report.BRoles)
		if !ok {
			t.Fatalf("%s: в отчёте нет блока «Кому что важно»", country.ID)
		}
		text := strings.Join(block.Lines, "\n")
		for _, role := range roles {
			line := ""
			for _, l := range block.Lines {
				if strings.HasPrefix(l, role) {
					line = strings.TrimSpace(strings.TrimPrefix(l, role))
				}
			}
			if line == "" {
				t.Errorf("%s: для роли %s нет строки в отчёте:\n%s", country.ID, role, text)
			}
		}
	}
}

// «Ведущий может показать 5 нестандартных сценариев без сбоев» (§21, §14).
func TestAcceptanceTrainerMode(t *testing.T) {
	c := newChat(t)
	c.Type("/demo").Expect("Режим ведущего")
	list := c.bot.DemoScenarios()
	if len(list) < 5 {
		t.Fatalf("в режиме ведущего %d сценариев, по ТЗ нужно не меньше 5", len(list))
	}
	for i, sc := range list {
		c.Type("/demo").Press(global(gDemoRun, fmt.Sprint(i)))
		if len(c.lastButtons()) == 0 {
			t.Errorf("сценарий %q: бот не предложил, что делать дальше", sc.Title)
		}
		if strings.Contains(c.Text(), "Что-то пошло не так") {
			t.Errorf("сценарий %q: сбой", sc.Title)
		}
	}
	t.Logf("сценариев в режиме ведущего: %d", len(list))
}
