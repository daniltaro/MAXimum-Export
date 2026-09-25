package maxbot

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"maxexport/internal/flow"
	"testing"

	maxapi "github.com/max-messenger/max-bot-api-client-go/v2"
)

func TestToHTML(t *testing.T) {
	cases := map[string]string{
		"**Шаг 1 из 4**":         "<b>Шаг 1 из 4</b>",
		"Pb ≤ 0,5 & <b>":         "Pb ≤ 0,5 &amp; &lt;b&gt;",
		"**незакрытый":           "<b>незакрытый</b>",
		"Итого: **0 ₽** и **1**": "Итого: <b>0 ₽</b> и <b>1</b>",
		"без разметки":           "без разметки",
	}
	for in, want := range cases {
		if got := toHTML(in); got != want {
			t.Errorf("toHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

// Повторяем только временные ошибки: лимит, сбой MAX, сеть. Отказ в доступе — не повторяем.
func TestTemporary(t *testing.T) {
	cases := map[error]bool{
		&maxapi.Error{Code: "too.many.requests"}:    true,
		&maxapi.Error{Code: "attachment.not.ready"}: true,
		&maxapi.Error{Code: "service.unavailable"}:  true,
		&maxapi.Error{Code: "verify.token"}:         false,
		&maxapi.Error{Code: "chat.not.found"}:       false,
		errors.New("connection reset by peer"):      true,
	}
	for err, want := range cases {
		if got := temporary(err); got != want {
			t.Errorf("temporary(%v) = %v, want %v", err, got, want)
		}
	}
}

// В журнал не должна попадать подписанная ссылка на загрузку файла.
func TestCleanErrHidesURL(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "https://upload.max.ru/upload?sig=secret123", Err: errors.New("timeout")}
	if got := cleanErr(err).Error(); strings.Contains(got, "secret123") || !strings.Contains(got, "timeout") {
		t.Errorf("cleanErr = %q", got)
	}
}

// Слишком длинное сообщение MAX отклоняет целиком, поэтому адаптер режет его сам:
// части укладываются в лимит, текст не теряется, кнопки и файл остаются у последней части.
func TestSplitLongMessage(t *testing.T) {
	line := strings.Repeat("длинная строка отчёта ", 10) // ~220 символов
	long := strings.Repeat(line+"\n", 40)                // ~8 800 символов
	msgs := split([]flow.Message{
		{Text: "короткое"},
		{Text: long, Buttons: [][]flow.Button{{{Text: "Назад", Payload: "g:start"}}}, File: &flow.File{Name: "a.txt"}},
	})
	if len(msgs) < 4 {
		t.Fatalf("длинное сообщение не разрезано: %d частей", len(msgs))
	}
	var restored []string
	for i, m := range msgs {
		if n := utf8.RuneCountInString(toHTML(m.Text)); n > MaxTextLen {
			t.Errorf("часть %d длиной %d символов — MAX отклонит", i+1, n)
		}
		last := i == len(msgs)-1
		if (m.Buttons != nil) != last || (m.File != nil) != last {
			t.Errorf("часть %d: кнопки и файл должны быть только у последней части", i+1)
		}
		if i > 0 {
			restored = append(restored, m.Text)
		}
	}
	if got := strings.Join(restored, "\n"); got != long {
		t.Errorf("текст после разрезания изменился: было %d символов, стало %d",
			utf8.RuneCountInString(long), utf8.RuneCountInString(got))
	}
	// Строка без переносов длиннее лимита тоже режется, символы не рвутся пополам.
	parts := splitText(strings.Repeat("ё", MaxTextLen+100))
	if len(parts) != 2 || !utf8.ValidString(parts[0]) || !utf8.ValidString(parts[1]) {
		t.Errorf("сплошная строка разрезана неверно: %d частей", len(parts))
	}
	// Экранирование учитывается: после toHTML «&» превращается в «&amp;».
	for i, p := range splitText(strings.Repeat("&\n", MaxTextLen)) {
		if n := utf8.RuneCountInString(toHTML(p)); n > MaxTextLen {
			t.Errorf("часть %d после экранирования длиной %d символов", i+1, n)
		}
	}
}
