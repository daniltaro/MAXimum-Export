package flow

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Три сценария ТЗ §22 — ровно так, как их пройдёт тренер
// ---------------------------------------------------------------------------

// Сценарий 1. Китай — сахар-песок (менеджер по ВЭД).
func TestScenario1ChinaSugar(t *testing.T) {
	c := newChat(t)
	c.Start().Expect("MAXimum Export — бот для экспорта", "Китай, 🇦🇲 Армения").ExpectButtons("Начать расчёт", "Пример заполнения", "Помощь")
	c.Click("Начать расчёт").Expect("Шаг 1 из 4")
	c.Click("Китай").Expect("✅ Страна: 🇨🇳 Китай", "Шаг 2 из 4")
	c.Type("сахар-песок").Expect("1. 1701 12 100 0 — Сахар-песок свекловичный")
	c.Click("1701 12 100 0").Expect("✅ Код 1701 12 100 0", "определён автоматически", "Шаг 3 из 4", "Сколько мешков?")
	c.Type("5000").Expect("✅ Количество: 5 000 мешков", "Вес партии брутто")
	c.Type("250000").Expect("✅ Вес: 250 000 кг (250 т)", "Шаг 4 из 4")
	c.Click("Рассчитать")
	// Все блоки по ТЗ §15 + дисклеймер, отчёт частями «1/N».
	c.Expect("Результат расчёта** (1/", "📦 Параметры сделки", "✅ Упаковка", "✅ Продукция", "✅ Маркировка",
		"📋 Сертификаты и разрешения", "🔬 Лабораторные испытания", "💰 Таможенная пошлина", "⚠️ Внимание",
		"Итого к уплате: **0 ₽**", "GACC", "2–6 мес.", "демонстрационный прототип")
	// Файл .txt приходит сразу вместе с отчётом.
	if f := c.File(); f == nil || !strings.HasSuffix(f.Name, ".txt") || !strings.Contains(string(f.Content), "ПАРАМЕТРЫ СДЕЛКИ") {
		t.Fatalf("после отчёта должен прийти файл .txt, получено %+v", f)
	}
	c.ExpectButtons("Скопировать отчёт", "Скачать отчёт", "Новый расчёт", "Изменить данные", "Задать вопрос")
	for _, m := range c.last {
		if n := utf8.RuneCountInString(m.Text); n > 4000 {
			t.Errorf("сообщение длиннее 4000 символов (лимит MAX): %d", n)
		}
	}

	// «Какие сертификаты?», «Сколько пошлина?», «Сколько ждать регистрацию?» — что проверяет тренер.
	c.Click("Задать вопрос").Expect("Ваш расчёт: Сахар-песок свекловичный → Китай")
	c.Type("Какие сертификаты нужны?").Expect("GACC")
	c.Type("Сколько пошлина?").Expect("0 ₽")
	c.Type("Сколько ждать регистрацию?").Expect("2–6")
	c.Click("К результату").ExpectButtons("Скопировать отчёт")

	// «Скопировать отчёт» — одно сообщение.
	c.Click("Скопировать отчёт").Expect("сводка расчёта", "Полный отчёт — в файле")
	if n := utf8.RuneCountInString(c.last[0].Text); n > 4000 {
		t.Errorf("сводка для копирования длиннее одного сообщения: %d", n)
	}
	c.Click("Скачать отчёт")
	if c.File() == nil {
		t.Error("«Скачать отчёт» должен прислать файл")
	}
}

// Сценарий 2. Армения — мёд натуральный (главный бухгалтер ВЭД).
func TestScenario2ArmeniaHoney(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Армения").Expect("ЕАЭС")
	c.Type("мёд натуральный").Click("0409 00 000 0")
	c.Type("200").Type("4000").Click("Рассчитать")
	c.Expect("не применяется (ЕАЭС)", "НДС: 0", "Статистическая форма", "Курс валюты: не требуется",
		"Ветеринарный сертификат", "в реестре предприятий ЕАЭС", "левомицетин")
	c.ExpectNot("Альтернативные рынки")
}

// Сценарий 3. Казахстан — мука пшеничная с опечаткой и ошибкой веса (эксперт по таможне).
func TestScenario3KazakhstanFlour(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Казахстан")
	c.Type("мука пшениичная").Expect("1101 00 000 0")
	c.Click("1101 00 000 0")
	c.Type("1000")
	c.Type("5").Expect("0,005 кг", "нетипичен", "Вы указали 5 — это 5 кг или 5 тонн?").
		ExpectButtons("5 кг", "5 тонн (5 000 кг)", "Изменить данные")
	c.Click("Изменить данные").Expect("Вес партии брутто") // страна и код сохранены
	c.Type("50000").Expect("✅ Вес: 50 000 кг", "Казахстан", "1101 00 000 0", "1 000 мешков")
	c.Click("Рассчитать").Expect("не применяется (ЕАЭС)", "Фитосанитарный сертификат", "клейковин", "казахск")
}

// ---------------------------------------------------------------------------
// Особые состояния экранов (ТЗ §15) и нестандартные ситуации (§14)
// ---------------------------------------------------------------------------

func TestCountryText(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт")
	c.Type("КНР").Expect(`Вы указали "КНР". Это Китай? Подтвердите.`).ExpectButtons("Да, это Китай", "Нет, выбрать другую")
	c.Click("Нет, выбрать другую").Expect("Шаг 1 из 4")
	c.Type("Турция").Expect("Бот поддерживает экспорт в три страны", "«Турция»").ExpectButtons("Китай", "Армения", "Казахстан")
	c.Type("Китай").Expect("✅ Страна: 🇨🇳 Китай", "Шаг 2 из 4") // точное название — без подтверждения
}

func TestProductSearchStates(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай")
	c.Type("сахар").Expect("Найдено много вариантов")
	c.Type("абракадабра").Expect("не найдены").ExpectButtons("Указать код вручную")
	c.Type("8471 30 000 0").Expect("не к пищевой продукции")
	c.Type("9999 99 999 9").Expect("не найден").ExpectButtons("Ввести другой код", "Подобрать по названию")
	c.Type("0403 10 110 0").Expect("заменён на 0403 20 110 0", "Шаг 3 из 4")
}

func TestManualCodeAndPrefix(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Click("Указать код вручную").Expect("10 цифр")
	c.Type("0409").Expect("Коды, которые начинаются на 0409").Click("0409 00 000 0").Expect("Шаг 3 из 4")
	c.ExpectNot("определён автоматически") // код введён вручную
}

func TestQuantityAndWeightErrors(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Type("сахар-песок").Click("1701 12 100 0")
	c.Type("0").Expect("Количество должно быть больше нуля")
	c.Type("-5").Expect("Количество должно быть больше нуля")
	c.Type("2,5").Expect("целое число")
	c.Type("много").Expect("Не вижу числа")
	c.Type("5000 мешков по 50 кг").Expect("По вашим данным: 5 000 × 50 кг = 250 000 кг").ExpectButtons("250 000 кг (5 000 × 50 кг)")
	c.Type("0").Expect("Вес должен быть больше нуля")
	c.Type("250000 / 260000").Expect("нетто не может быть больше")
	c.Type("250").Expect("Вы указали 250 — это 250 кг или 250 тонн?") // ТЗ §15, экран 4
	c.Click("250 тонн").Expect("✅ Вес: 250 000 кг", "Шаг 4 из 4")
}

func TestSkipWeightAndDate(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Type("пшеница мягкая").Click("1001 99 000 0")
	c.Type("100").Click("Пропустить вес").Expect("Вес пропущен", "Вес: не указан")
	c.Click("Дата отгрузки").Type("31.02.2027").Expect("Не удалось распознать дату")
	c.Type("01.01.2020").Expect("уже прошла")
	c.Type("01.03.2027").Expect("✅ Плановая отгрузка: 01.03.2027")
	c.Click("Рассчитать").Expect("Для расчёта пошлины по этой ставке укажите вес", "экспортная квота")
}

func TestEditKeepsCountryAndCode(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Type("сахар-песок").Click("1701 12 100 0").Type("5000").Type("250000")
	c.Click("✏️ Изменить").Click("Количество").Type("4000").Expect("Шаг 4 из 4", "4 000 мешков", "Китай", "1701 12 100 0")
	c.Click("✏️ Изменить").Click("Страна").Click("Армения").Expect("Шаг 4 из 4", "Армения", "1701 12 100 0")
	c.Click("Рассчитать").Click("Изменить данные").Expect("Что изменить?")
	c.Click("Вес").Type("200000").Expect("Шаг 4 из 4", "200 000 кг", "Армения", "4 000 мешков")
}

func TestBackFromEveryStep(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Назад").Expect("MAXimum Export — бот")
	c.Click("Начать расчёт").Click("Китай").Click("Назад").Expect("Шаг 1 из 4")
	c.Click("Китай").Type("сахар-песок").Click("1701 12 100 0").Click("Назад").Expect("Шаг 2 из 4")
	c.Type("сахар-песок").Click("1701 12 100 0").Type("5000").Click("Назад").Expect("Шаг 3 из 4", "Сколько мешков?")
	c.Type("5000").Type("250000").Click("Назад").Expect("Вес партии брутто")
}

func TestStaleButton(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт")
	old := c.payloadOf("Армения")
	c.Click("Китай").Type("сахар-песок")
	c.Press(old).Expect("Эта кнопка устарела")
	c.Click("1701 12 100 0").Expect("Шаг 3 из 4", "Китай") // страна не сбилась
}

func TestPIIAndOtherInput(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай")
	c.Type("сахар, мой телефон +7 (999) 123-45-67").Expect("номер телефона", "не принимает персональные").ExpectNot("Найдено")
	c.Unsupported().Expect("понимает только текст и кнопки")
	c.Type("/foo").Expect("Неизвестная команда")
	c.Type(strings.Repeat("а", 300)).Expect("слишком длинное")
	c.Type("/help").Expect("Справка", "Что такое код ТН ВЭД?").Click("Назад").Expect("Шаг 2 из 4")
}

func TestExportBanAndAlternatives(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Type("рис-сырец")
	c.Click("1. ").Type("20").Type("20000").Click("Рассчитать")
	c.Expect("🚫 Экспорт невозможен", "Подача декларации невозможна").ExpectNot("✅ Упаковка")
	c.Click("Альтернативные рынки").Expect("Повторить расчёт для другой страны").ExpectButtons("Армения", "Казахстан")
	c.Click("Казахстан").Expect("не применяется (ЕАЭС)").ExpectNot("🚫 Экспорт невозможен")
}

func TestReturningUserAndRepeat(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Пример заполнения").Expect("Пример заполнения на учебных данных", "Результат расчёта")
	c.Start().Expect("С возвращением! Ваш последний расчёт — Сахар-песок свекловичный → Китай от 21.09.2026")
	c.Click("Повторить расчёт").Expect("Шаг 4 из 4", "5 000 мешков")
	c.Click("Рассчитать").Expect("Результат расчёта")
}

func TestOldResultButtonsStillWork(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Пример заполнения")
	copyFirst := c.payloadOf("Скопировать отчёт")
	c.Click("Новый расчёт").Click("Армения").Type("мёд натуральный").Click("0409").Type("200").Type("4000").Click("Рассчитать")
	c.Press(copyFirst).Expect("Сахар-песок свекловичный") // копируется именно тот, старый расчёт
}

// Режим ведущего: каждый сценарий отрабатывает без сбоев (ТЗ §21).
func TestDemoScenarios(t *testing.T) {
	c := newChat(t)
	c.Type("/demo").Expect("Режим ведущего")
	want := map[string]string{
		"Запрет вывоза": "🚫 Экспорт невозможен", "Квота на зерно": "экспортная квота", "Запрет ввоза": "Казахстан запретил ввоз",
		"Устаревший код": "заменён на 0403 20 110 0", "Сбой курса": "Не удалось получить актуальный курс", "Скачок курса": "изменился на",
		"Профиль недоступен": "Профиль требований", "Данные устарели": "Данные обновлены на", "Рефрижератор": "температурного режима",
		"Спорный код": "может быть отнесён", "Мак в составе": "мак", "Анализы не успеют": "лабораторных испытаний", "Код сменится": "заменён на",
		"Мука: вес": "5 кг или 5 тонн", "Сбой справочника": "Шаг 2 из 4", // сам сбой проверяется в TestDemoTnvedDown
	}
	// Сценарии, которые открывают шаг ввода, а не готовый отчёт.
	noReport := map[string]bool{"Мука": true, "Сбой справочника": true}
	for i, sc := range c.bot.DemoScenarios() {
		c.Press(global(gDemoRun, fmt.Sprint(i)))
		withReport := true
		for key := range noReport {
			if strings.Contains(sc.Title, key) {
				withReport = false
			}
		}
		if withReport && c.File() == nil {
			t.Errorf("сценарий %q: нет результата с файлом", sc.Title)
		}
		for key, frag := range want {
			if strings.Contains(sc.Title, key) && !strings.Contains(c.Text(), frag) {
				t.Errorf("сценарий %q: в ответе нет %q", sc.Title, frag)
			}
		}
		if n := utf8.RuneCountInString(sc.Title); n > 30 {
			t.Errorf("надпись кнопки сценария длиннее 30 символов: %q", sc.Title)
		}
	}
}

// Все подписи кнопок, которые видит пользователь, не длиннее 30 символов (MAX обрезает длинные).
// Ревью: устаревший код, введённый вручную, попадает в отчёт предупреждением (§14.1).
func TestReplacedCodeWarningInReport(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Click("Указать код вручную")
	c.Type("0403 10 110 0").Expect("заменён на 0403 20 110 0")
	c.Type("500").Type("6000").Click("Рассчитать")
	c.Expect("Код 0403 10 110 0 заменён на 0403 20 110 0", "Расчёт выполнен по новому коду")
	if f := c.File(); f == nil || !strings.Contains(string(f.Content), "заменён на 0403 20 110 0") {
		t.Error("в файле отчёта нет предупреждения о замене кода")
	}
}

// Демо «Сбой справочника»: §14.8 — справочник недоступен, бот предлагает повторить.
func TestDemoTnvedDown(t *testing.T) {
	c := newChat(t)
	c.Type("/demo").Click("Сбой справочника").Expect("Шаг 2 из 4")
	c.Type("сахар-песок").Expect("Справочник ТН ВЭД временно недоступен").ExpectButtons("Повторить запрос", "Указать код вручную")
	c.Click("Указать код вручную").Type("1701121000").Expect("Справочник ТН ВЭД временно недоступен")
	// Новый расчёт снимает демо-режим.
	c.Type("/start").Click("Начать расчёт").Click("Китай").Type("сахар-песок").Expect("1701 12 100 0")
}

func TestButtonLabelsFit(t *testing.T) {
	c := newChat(t)
	c.Start().Click("Начать расчёт").Click("Китай").Type("сахар-песок").Click("1701 12 100 0").Type("5000 мешков по 50 кг")
	c.Click("250 000 кг").Click("Рассчитать").Click("Задать вопрос").Type("что писать на этикетке")
	for _, m := range c.all {
		for _, r := range m.Buttons {
			for _, b := range r {
				if n := utf8.RuneCountInString(b.Text); n > 30 {
					t.Errorf("кнопка длиннее 30 символов: %q", b.Text)
				}
				if len(b.Payload) > 64 {
					t.Errorf("payload длиннее 64 байт: %q", b.Payload)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Устойчивость: случайные нажатия и сообщения никогда не ломают бота (ТЗ §21)
// ---------------------------------------------------------------------------

func TestFuzzNeverStuck(t *testing.T) {
	c := newChat(t)
	inputs := []string{"сахар-песок", "Китай", "КНР", "5000", "250", "0", "мёд", "1701", "01.03.2027", "абв", "/help", "/start", "/demo",
		"Сколько пошлина?", "250000 / 248000", "-1", "мука пшеничная", "Армения", ""}
	c.Start()
	seed := uint32(7)
	next := func(n int) int { seed = seed*1664525 + 1013904223; return int(seed>>8) % n }
	start := time.Now()
	for i := 0; i < 1500; i++ {
		btns := c.lastButtons()
		switch {
		case len(btns) > 0 && next(3) > 0:
			c.Press(btns[next(len(btns))].Payload)
		default:
			c.Type(inputs[next(len(inputs))])
		}
		if len(c.lastButtons()) == 0 && !strings.Contains(c.Text(), "Шаг") && !strings.Contains(c.Text(), "Введите") &&
			!strings.Contains(c.Text(), "Напишите") && !strings.Contains(c.Text(), "⚠️") && !strings.Contains(c.Text(), "Сколько") {
			t.Fatalf("шаг %d: бот не предложил, что делать дальше:\n%s", i, c.Text())
		}
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("1500 действий заняли %v", d)
	}
}

func (c *chat) lastButtons() []Button {
	var out []Button
	for _, m := range c.last {
		for _, r := range m.Buttons {
			out = append(out, r...)
		}
	}
	return out
}

// ТЗ §21: «Время отклика ≤ 5 секунд; 20 расчётов подряд — без сбоев».
func TestTwentyCalculationsInARow(t *testing.T) {
	c := newChat(t)
	start := time.Now()
	for i := 0; i < 20; i++ {
		c.Start()
		if i > 0 {
			c.Click("Новый расчёт")
		} else {
			c.Click("Начать расчёт")
		}
		c.Click("Китай").Type("сахар-песок").Click("1701 12 100 0").Type(fmt.Sprint(1000 + i)).Type(fmt.Sprint(50000 + i*50)).Click("Рассчитать")
		c.Expect("Результат расчёта")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("20 полных расчётов заняли %v", d)
	}
}
