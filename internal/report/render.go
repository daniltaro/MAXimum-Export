package report

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"maxexport/internal/engine"
)

// ChatLimit — длина одной части отчёта в чате (ТЗ §21: «при длине > 3500 символов —
// разбивка на части»). У MAX жёсткий лимит 4000 символов на сообщение.
const ChatLimit = 3500

// ChatParts — отчёт для чата: одна или несколько частей не длиннее limit символов.
// Разрезаем только между блоками, чтобы блок не разрывался (кроме очень длинного блока).
// Части подписываются «(1/2)», «(2/2)». Дисклеймер — в конце последней части.
func (rep Report) ChatParts(limit int) []string {
	var chunks []string // готовые фрагменты: заголовок, блоки, дисклеймер
	chunks = append(chunks, "**Результат расчёта**")
	for _, b := range rep.Blocks {
		chunks = append(chunks, renderBlock(b, true))
	}
	chunks = append(chunks, Disclaimer)

	const suffixRoom = 40 // место под заголовок «**Результат расчёта** (2/3)» в начале части
	var parts []string
	cur := ""
	for _, ch := range chunks {
		for _, piece := range splitLong(ch, limit-suffixRoom) {
			if cur != "" && runes(cur)+2+runes(piece) > limit-suffixRoom {
				parts = append(parts, cur)
				cur = ""
			}
			if cur != "" {
				cur += "\n\n"
			}
			cur += piece
		}
	}
	if cur != "" {
		parts = append(parts, cur)
	}
	if len(parts) > 1 {
		for i := range parts {
			label := fmt.Sprintf("(%d/%d)", i+1, len(parts))
			if i == 0 {
				parts[i] = strings.Replace(parts[i], "**Результат расчёта**", "**Результат расчёта** "+label, 1)
			} else {
				parts[i] = "**Результат расчёта** " + label + "\n\n" + parts[i]
			}
		}
	}
	return parts
}

// Plain — компактная версия для кнопки «📋 Скопировать отчёт»: без разметки,
// без блока ролей и информационных пометок, чтобы уместиться в одно сообщение.
func (rep Report) Plain(limit int) []string {
	var b strings.Builder
	b.WriteString("MAXimum Export — результат расчёта\n\n")
	for _, bl := range rep.Blocks {
		if bl.ID == BRoles {
			continue
		}
		if bl.ID == BWarnings {
			bl.Lines = onlyImportant(bl.Lines)
			if len(bl.Lines) == 0 {
				continue
			}
		}
		b.WriteString(stripMarkup(renderBlock(bl, false)))
		b.WriteString("\n\n")
	}
	b.WriteString(Disclaimer)
	return splitLong(b.String(), limit)
}

// onlyImportant оставляет предупреждения ⚠️ и 🚫, убирая справочные ℹ️.
func onlyImportant(lines []string) []string {
	var out []string
	for _, l := range lines {
		if !strings.HasPrefix(l, "ℹ️") {
			out = append(out, l)
		}
	}
	return out
}

// renderBlock печатает блок: заголовок и строки с тире. Многострочные пункты
// (например, варианты классификации) печатаются с отступом.
func renderBlock(b Block, bold bool) string {
	var sb strings.Builder
	if bold {
		sb.WriteString("**" + b.Title + "**")
	} else {
		sb.WriteString(b.Title)
	}
	for _, l := range b.Lines {
		sb.WriteString("\n— ")
		sb.WriteString(strings.ReplaceAll(l, "\n", "\n   "))
	}
	return sb.String()
}

// stripMarkup убирает разметку **жирный**.
func stripMarkup(s string) string {
	return strings.ReplaceAll(s, "**", "")
}

// splitLong режет слишком длинный текст по строкам на куски не длиннее limit символов.
func splitLong(s string, limit int) []string {
	if runes(s) <= limit {
		return []string{s}
	}
	var out []string
	cur := ""
	for _, line := range strings.Split(s, "\n") {
		for runes(line) > limit { // одна строка длиннее лимита — режем по словам
			cut := cutAtSpace(line, limit)
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			out = append(out, line[:cut])
			line = strings.TrimLeft(line[cut:], " ")
		}
		if cur != "" && runes(cur)+1+runes(line) > limit {
			out = append(out, cur)
			cur = ""
		}
		if cur != "" {
			cur += "\n"
		}
		cur += line
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// cutAtSpace — байтовая позиция последнего пробела в пределах limit символов.
func cutAtSpace(s string, limit int) int {
	pos, lastSpace, n := 0, -1, 0
	for i, r := range s {
		if n == limit {
			pos = i
			break
		}
		if r == ' ' {
			lastSpace = i
		}
		n++
		pos = i + utf8.RuneLen(r)
	}
	if lastSpace > 0 {
		return lastSpace
	}
	return pos
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// ---------------------------------------------------------------------------
// Текстовый документ .txt (ТЗ §11: «отдельный текстовый документ»)
// ---------------------------------------------------------------------------

// FileName — имя файла отчёта: MAXimum-Export_CN_1701121000_2026-09-21.txt
func (rep Report) FileName() string {
	r := rep.Result
	return fmt.Sprintf("MAXimum-Export_%s_%s_%s.txt",
		strings.ToUpper(r.Country.ID), r.Product.Code, r.At.In(engine.MSK).Format("2006-01-02"))
}

// Text — полный отчёт для скачивания: таблица параметров, все блоки, все пометки
// о проверке человеком (ТЗ §20) и дисклеймер.
func (rep Report) Text() string {
	r := rep.Result
	var b strings.Builder
	line := strings.Repeat("=", 72)

	b.WriteString("MAXimum Export — отчёт по экспорту пищевой продукции\n")
	b.WriteString(line + "\n")
	fmt.Fprintf(&b, "Дата расчёта: %s   Данные справочников на: %s   УЧЕБНЫЕ ДАННЫЕ\n\n",
		engine.FormatDate(r.At), engine.FormatDate(r.DataAsOf))

	for _, bl := range rep.Blocks {
		b.WriteString(strings.ToUpper(stripEmoji(bl.Title)) + "\n")
		if bl.ID == BParams {
			b.WriteString(table(bl.Lines))
		} else {
			for _, l := range bl.Lines {
				b.WriteString("  - " + strings.ReplaceAll(stripMarkup(l), "\n", "\n    ") + "\n")
			}
		}
		b.WriteString("\n")
	}

	if len(r.Country.Logistics) > 1 {
		b.WriteString("ЛОГИСТИКА И МАРШРУТ\n")
		for _, l := range r.Country.Logistics {
			b.WriteString("  - " + l + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("ГДЕ НУЖНА ПРОВЕРКА ЧЕЛОВЕКА\n")
	for _, l := range humanChecks(r) {
		b.WriteString("  - " + l + "\n")
	}
	b.WriteString("\n" + line + "\n" + Disclaimer + "\n")
	return b.String()
}

// humanChecks — пункты ТЗ §20 с подставленными датами.
func humanChecks(r engine.Result) []string {
	asOf := engine.FormatDate(r.DataAsOf)
	return []string{
		"Классификация по ТН ВЭД: код определён автоматически, окончательную классификацию подтверждает таможенный орган. При спорных случаях получите классификационное решение ФТС.",
		"Ставка пошлины актуальна на дату расчёта. Перед подачей декларации проверьте ставку в ТКС ЕАЭС или у таможенного брокера.",
		"Требования страны назначения приведены по состоянию на " + asOf + ". Проверьте актуальность: " + r.Country.Authority + ".",
		"Срок регистрации предприятия (" + r.Country.Registry.Months + " мес.) ориентировочный: он зависит от типа продукции и полноты документов. Уточняйте: " + r.Country.Registry.Who + ".",
		"Информация об ограничениях обновлена на " + asOf + ". Перед сделкой проверьте актуальный статус на сайте ФТС или Минсельхоза.",
		"Курс валюты: в MVP используется учебный курс (1 евро = 100 ₽) или курс ЦБ РФ. Для декларации применяется курс ЦБ РФ на дату её регистрации.",
	}
}

// table печатает строки «Ключ: значение» таблицей с рамкой.
func table(lines []string) string {
	type row struct{ k, v string }
	var rows []row
	kw, vw := 0, 0
	for _, l := range lines {
		k, v, _ := strings.Cut(stripEmoji(l), ": ")
		rows = append(rows, row{k, v})
		kw, vw = max(kw, runes(k)), max(vw, runes(v))
	}
	border := "  +" + strings.Repeat("-", kw+2) + "+" + strings.Repeat("-", vw+2) + "+\n"
	var b strings.Builder
	b.WriteString(border)
	for _, r := range rows {
		fmt.Fprintf(&b, "  | %s%s | %s%s |\n", r.k, strings.Repeat(" ", kw-runes(r.k)), r.v, strings.Repeat(" ", vw-runes(r.v)))
	}
	b.WriteString(border)
	return b.String()
}

// stripEmoji убирает значки и флаги (в .txt они часто отображаются квадратиками).
// Остальные символы (≤, ≥, №, китайские иероглифы на этикетке) сохраняются.
func stripEmoji(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF: // эмодзи и флаги: 📦 💰 🇨🇳
		case r >= 0x2600 && r <= 0x27BF: // значки: ⚠ ✅
		case r == 0x2139 || r == 0xFE0F || r == 0x200D: // ℹ и служебные символы эмодзи
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}
