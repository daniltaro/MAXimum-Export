// Package texts — все тексты интерфейса бота и мини-приложения (файл ru.json рядом).
//
// Чтобы поменять формулировку, эмодзи или надпись на кнопке, отредактируйте ru.json —
// код трогать не нужно. Подстановки пишутся в фигурных скобках: «Шаг {step} из {total}».
// Описание, какой текст на каком экране, — в docs/screens.md.
//
// Тексты предупреждений отчёта сюда не входят: их формирует ядро
// (internal/engine/warnings.go), потому что в них подставляются расчётные данные.
package texts

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

//go:embed ru.json
var ruJSON []byte

var ru map[string]string

func init() {
	if err := json.Unmarshal(ruJSON, &ru); err != nil {
		panic(fmt.Sprintf("texts/ru.json повреждён: %v", err))
	}
}

// T возвращает текст по ключу с подстановками: T("common.step", "step", "1", "total", "4").
// Если ключа нет — возвращает «[[ключ]]», чтобы ошибку было видно сразу (а тест её ловит).
func T(key string, pairs ...string) string {
	s, ok := ru[key]
	if !ok {
		return "[[" + key + "]]"
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "{"+pairs[i]+"}", pairs[i+1])
	}
	return s
}

// Has — есть ли такой ключ.
func Has(key string) bool {
	_, ok := ru[key]
	return ok
}

// All — копия всех текстов (для мини-приложения: GET /api/v1/texts).
func All() map[string]string { return maps.Clone(ru) }
