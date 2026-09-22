package texts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Каждый ключ, который код передаёт в texts.T("..."), должен существовать в ru.json.
// Тест просматривает исходники проекта, поэтому опечатка в ключе ловится сразу.
func TestAllUsedKeysExist(t *testing.T) {
	re := regexp.MustCompile(`texts\.T\("([a-z0-9_.]+)"`)
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			if strings.HasSuffix(m[1], ".") {
				continue // ключ собирается из частей («help.article.» + id) — проверяется в тестах пакета
			}
			if !Has(m[1]) {
				t.Errorf("%s: ключа %q нет в texts/ru.json", path, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSubstitution(t *testing.T) {
	if got := T("common.step", "step", "1", "total", "4"); got != "Шаг 1 из 4" {
		t.Errorf("подстановка: %q", got)
	}
	if got := T("no.such.key"); got != "[[no.such.key]]" {
		t.Errorf("отсутствующий ключ: %q", got)
	}
}

// Надписи на кнопках в MAX обрезаются, если длинные (ключи btn.*). Подстановки {…}
// заменяем 5-символьным значением — примерно как «5 000» или «тонн».
func TestButtonLabelsShort(t *testing.T) {
	placeholder := regexp.MustCompile(`\{[a-z_]+\}`)
	for k, v := range All() {
		label := placeholder.ReplaceAllString(v, "12345")
		if strings.HasPrefix(k, "btn.") && len([]rune(label)) > 30 {
			t.Errorf("кнопка %s длиннее 30 символов: %q", k, label)
		}
	}
}
