// Консольный чат: тот же бот, что и в MAX, но прямо в терминале — без мессенджера и интернета.
//
//	go run ./cmd/chat
//
// Бот печатает сообщения и пронумерованные кнопки. Введите номер кнопки, чтобы нажать её,
// или обычный текст. Команды: /start, /help, /demo. Выход — Ctrl+C или /exit.
// Файлы отчёта сохраняются в папку reports/ рядом с программой.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/flow"
	"maxexport/internal/service"
)

func main() {
	bot := flow.New(service.New(data.MustLoad(), engine.TrainingSource{}))
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64*1024), 64*1024)

	fmt.Println("Консольный чат MAXimum Export. Номер — нажать кнопку, текст — написать боту, /exit — выход.")
	buttons := show(bot.Handle("console", flow.Start()))
	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			return
		}
		text := strings.TrimSpace(in.Text())
		switch {
		case text == "/exit":
			return
		case text == "":
			continue
		}
		var input flow.Input
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(buttons) {
			b := buttons[n-1]
			if b.OpenApp != "" {
				fmt.Println("(кнопка открывает мини-приложение в MAX: " + b.OpenApp + ")")
				continue
			}
			fmt.Println("  [нажато: " + b.Text + "]")
			input = flow.Action(b.Payload)
		} else {
			input = flow.Text(text)
		}
		buttons = show(bot.Handle("console", input))
	}
}

// show печатает сообщения бота и возвращает кнопки последнего ответа (по порядку, с номерами).
func show(msgs []flow.Message) []flow.Button {
	var all []flow.Button
	for _, m := range msgs {
		fmt.Println("\n────────────────────────────────────────")
		fmt.Println(strings.ReplaceAll(m.Text, "**", ""))
		if m.File != nil {
			path := saveFile(m.File)
			fmt.Printf("📎 Файл: %s (%d байт) — сохранён: %s\n", m.File.Name, len(m.File.Content), path)
		}
		for _, row := range m.Buttons {
			for _, b := range row {
				all = append(all, b)
				fmt.Printf("  [%d] %s\n", len(all), b.Text)
			}
		}
	}
	return all
}

func saveFile(f *flow.File) string {
	dir := "reports"
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, filepath.Base(f.Name))
	if err := os.WriteFile(path, f.Content, 0o644); err != nil {
		return "не удалось сохранить: " + err.Error()
	}
	abs, _ := filepath.Abs(path)
	return abs
}
