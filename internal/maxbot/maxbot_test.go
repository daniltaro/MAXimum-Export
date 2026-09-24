package maxbot

import (
	"errors"
	"net/url"
	"strings"
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
