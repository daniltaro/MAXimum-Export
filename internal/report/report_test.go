package report

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"maxexport/data"
	"maxexport/internal/engine"
)

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, engine.MSK)

func calc(t *testing.T, in engine.Input) Report {
	t.Helper()
	e := engine.New(data.MustLoad())
	return Build(e.Calculate(in, engine.Env{Now: now, Rates: engine.Training(now)}))
}

// ТЗ §15 (экран 5) и §22, сценарий 1: все 7 блоков в фиксированном порядке + дисклеймер.
func TestChinaReportBlocks(t *testing.T) {
	rep := calc(t, engine.Input{Country: "cn", Code: "1701121000", Qty: 5000, UnitWord: "мешков", WeightKg: 250000})
	want := []string{BParams, BPackaging, BProduct, BLabeling, BDocuments, BLab, BDuty, BRoles, BWarnings}
	var got []string
	for _, b := range rep.Blocks {
		got = append(got, b.ID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("порядок блоков: %v, ожидался %v", got, want)
	}

	params, _ := rep.Block(BParams)
	joined := strings.Join(params.Lines, "\n")
	for _, s := range []string{"Китай", "1701 12 100 0", "5 000 мешков", "250 000 кг (250 т)"} {
		if !strings.Contains(joined, s) {
			t.Errorf("в параметрах сделки нет %q:\n%s", s, joined)
		}
	}

	duty, _ := rep.Block(BDuty)
	dutyText := strings.Join(duty.Lines, "\n")
	for _, s := range []string{"Тип ставки: не установлена", "Итого к уплате: **0 ₽**", "НДС"} {
		if !strings.Contains(dutyText, s) {
			t.Errorf("в блоке пошлины нет %q:\n%s", s, dutyText)
		}
	}

	parts := rep.ChatParts(ChatLimit)
	for i, p := range parts {
		if n := utf8.RuneCountInString(p); n > ChatLimit {
			t.Errorf("часть %d длиной %d символов — больше %d", i+1, n, ChatLimit)
		}
	}
	if !strings.HasSuffix(parts[len(parts)-1], Disclaimer) {
		t.Error("дисклеймер MVP должен завершать последнюю часть отчёта")
	}
	if len(parts) > 1 && !strings.Contains(parts[0], "(1/") {
		t.Error("части отчёта должны быть подписаны «(1/2)», «(2/2)»")
	}
}

// ТЗ §22, сценарий 2: для ЕАЭС пошлина не применяется, курс не нужен, статформа.
func TestEAEUReport(t *testing.T) {
	rep := calc(t, engine.Input{Country: "am", Code: "0409000000", Qty: 200, WeightKg: 4000})
	duty, _ := rep.Block(BDuty)
	text := strings.Join(duty.Lines, "\n")
	for _, s := range []string{"не применяется (ЕАЭС)", "НДС: 0", "Статистическая форма", "Курс валюты: не требуется"} {
		if !strings.Contains(text, s) {
			t.Errorf("в блоке пошлины для ЕАЭС нет %q:\n%s", s, text)
		}
	}
}

// Запрет экспорта: требования заменяются крупным предупреждением.
func TestStopReport(t *testing.T) {
	e := engine.New(data.MustLoad())
	rice := e.Cat.ProductsWithPrefix("100610")[0]
	rep := calc(t, engine.Input{Country: "cn", Code: rice.Code, Qty: 20, WeightKg: 20000})
	if _, ok := rep.Block(BStop); !ok {
		t.Fatal("нет блока «Экспорт невозможен»")
	}
	if _, ok := rep.Block(BPackaging); ok {
		t.Error("при запрете требования к упаковке не показываются")
	}
}

// Каждое сочетание «код × страна»: части ≤ 3500, копия ≤ 4000, есть дисклеймер и 5 ролей.
func TestAllCombinations(t *testing.T) {
	cat := data.MustLoad()
	for _, c := range cat.Countries {
		for _, p := range cat.Products {
			if !p.Food {
				continue
			}
			rep := calc(t, engine.Input{Country: c.ID, Code: p.Code, Qty: 100, WeightKg: 30000, NetKg: 29000,
				ShipDate: now.AddDate(0, 0, 5)})
			for _, part := range rep.ChatParts(ChatLimit) {
				if utf8.RuneCountInString(part) > ChatLimit {
					t.Errorf("%s/%s: часть длиннее %d", c.ID, p.Code, ChatLimit)
				}
			}
			// «Скопировать отчёт» — всегда одно сообщение (ТЗ §15), с дисклеймером.
			if s := rep.Summary(CopyLimit); utf8.RuneCountInString(s) > CopyLimit || !strings.Contains(s, "демонстрационный прототип") {
				t.Errorf("%s/%s: сводка длиннее %d символов или без дисклеймера", c.ID, p.Code, CopyLimit)
			}
			if rep.Result.Stop == nil {
				roles, _ := rep.Block(BRoles)
				if len(roles.Lines) != 5 {
					t.Errorf("%s/%s: в блоке ролей %d строк, нужно 5", c.ID, p.Code, len(roles.Lines))
				}
			}
			txt := rep.Text()
			if !strings.Contains(txt, Disclaimer) || !strings.Contains(txt, "УЧЕБНЫЕ ДАННЫЕ") {
				t.Errorf("%s/%s: в .txt нет дисклеймера или пометки об учебных данных", c.ID, p.Code)
			}
		}
	}
}

func TestTextDocument(t *testing.T) {
	rep := calc(t, engine.Input{Country: "cn", Code: "1701121000", Qty: 5000, UnitWord: "мешков", WeightKg: 250000})
	txt := rep.Text()
	for _, s := range []string{"ПАРАМЕТРЫ СДЕЛКИ", "| Страна", "ГДЕ НУЖНА ПРОВЕРКА ЧЕЛОВЕКА", "ТАМОЖЕННАЯ ПОШЛИНА"} {
		if !strings.Contains(txt, s) {
			t.Errorf("в .txt нет %q", s)
		}
	}
	if rep.FileName() != "MAXimum-Export_CN_1701121000_2026-09-21.txt" {
		t.Errorf("имя файла: %s", rep.FileName())
	}
}

func TestSummaryContent(t *testing.T) {
	rep := calc(t, engine.Input{Country: "cn", Code: "1205109000", Qty: 100, WeightKg: 100000})
	s := rep.Summary(CopyLimit)
	for _, want := range []string{"сводка расчёта", "Семена рапса", "Документы:", "1 650 000 ₽", "21 344 ₽", "Полный отчёт — в файле"} {
		if !strings.Contains(s, want) {
			t.Errorf("в сводке нет %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "**") {
		t.Error("в сводке для копирования не должно быть разметки")
	}
}
