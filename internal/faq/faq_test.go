package faq

import (
	"strings"
	"testing"
	"time"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/report"
)

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, engine.MSK)

func build(t *testing.T, in engine.Input) report.Report {
	t.Helper()
	e := engine.New(data.MustLoad())
	return report.Build(e.Calculate(in, engine.Env{Now: now, Rates: engine.Training(now)}))
}

func TestHelpArticles(t *testing.T) {
	arts := Help()
	if len(arts) != 6 {
		t.Fatalf("в справке %d статей, ожидалось 6", len(arts))
	}
	for _, a := range arts {
		if strings.HasPrefix(a.Title, "[[") || strings.HasPrefix(a.Text, "[[") || a.Text == "" {
			t.Errorf("статья %s не заполнена", a.ID)
		}
	}
}

// По вопросу своими словами должна находиться нужная тема и часть отчёта.
func TestTopics(t *testing.T) {
	sugar := build(t, engine.Input{Country: "cn", Code: "1701121000", Qty: 5000, WeightKg: 250000})
	honey := build(t, engine.Input{Country: "am", Code: "0409000000", Qty: 200, WeightKg: 4000})
	cases := []struct {
		rep            report.Report
		question, want string
	}{
		{sugar, "Какие сертификаты нужны?", "Сертификаты"},
		{sugar, "Сколько пошлина?", "пошлина"},
		{sugar, "Сколько ждать регистрацию?", "Регистрация"},
		{sugar, "какие анализы сдать", "Лабораторные"},
		{sugar, "что писать на этикетке", "Маркировка"},
		{sugar, "по какому курсу евро считали", "Курс"},
		{sugar, "есть ли запреты или квоты", "Запреты"},
		{honey, "нужен ли ветеринарный сертификат", "Сертификаты"},
		{honey, "как вернуть ндс", "НДС"},
	}
	for _, c := range cases {
		a := Ask(c.rep, c.question)
		if !a.Found || !strings.Contains(a.Topic, c.want) {
			t.Errorf("вопрос %q: тема %q, ожидалась «%s…»", c.question, a.Topic, c.want)
		}
	}
	if a := Ask(honey, "Какие сертификаты нужны?"); !strings.Contains(a.Text, "Ветеринарный") {
		t.Errorf("мёд в Армению — в ответе о сертификатах нет ветсертификата: %s", a.Text)
	}
	// Ревью: «текст» не должен считаться опечаткой в «тест», «стоит ли» — не про пошлину.
	if a := Ask(sugar, "какой текст на этикетке"); a.TopicID != "labeling" {
		t.Errorf("«какой текст на этикетке»: тема %q", a.TopicID)
	}
	// При запрете экспорта любой ответ начинается с запрета.
	e := engine.New(data.MustLoad())
	rice := e.Cat.ProductsWithPrefix("100610")[0]
	banned := build(t, engine.Input{Country: "cn", Code: rice.Code, Qty: 20, WeightKg: 20000})
	if a := Ask(banned, "Сколько ждать регистрацию?"); !strings.Contains(a.Text, "🚫") {
		t.Errorf("при запрете экспорта ответ должен начинаться с запрета: %s", a.Text)
	}
	if a := Ask(sugar, "абракадабра"); a.Found || len(a.Suggestions) == 0 {
		t.Error("нераспознанный вопрос: нужны подсказки")
	}
}
