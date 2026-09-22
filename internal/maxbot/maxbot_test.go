package maxbot

import "testing"

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
