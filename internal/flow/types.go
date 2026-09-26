// Package flow — диалог чат-бота: шесть экранов и переходы между ними.
//
// Бот устроен как конечный автомат. У каждого пользователя есть сессия: на каком он
// экране и что уже ввёл (страна, код, количество, вес). На каждое событие вызывается
// Bot.Handle(пользователь, ввод) → сообщения, которые нужно отправить.
//
// Пакет ничего не знает о мессенджере: адаптер MAX (internal/maxbot) и консольный чат
// (cmd/chat) передают сюда события и отправляют то, что вернула Handle.
// Какой текст и какие кнопки на каком экране — описано в docs/screens.md,
// сами тексты — в texts/ru.json.
//
// Один файл — один экран: screen1_start.go … screen6_help.go.
package flow

// InputKind — вид события от пользователя.
type InputKind int

const (
	InputStart       InputKind = iota // пользователь открыл бота (bot_started) или отправил /start
	InputText                         // текстовое сообщение
	InputAction                       // нажатие кнопки
	InputUnsupported                  // файл, фото, голосовое — бот их не обрабатывает
)

// Input — событие от пользователя.
type Input struct {
	Kind    InputKind
	Text    string // для InputText
	Payload string // для InputAction: что зашито в кнопку
}

// Конструкторы событий — для адаптеров и тестов.
func Start() Input                { return Input{Kind: InputStart} }
func Text(s string) Input         { return Input{Kind: InputText, Text: s} }
func Action(payload string) Input { return Input{Kind: InputAction, Payload: payload} }
func Unsupported() Input          { return Input{Kind: InputUnsupported} }

// Button — кнопка под сообщением.
type Button struct {
	Text    string // надпись (MAX обрезает длинные — держим до 30 символов)
	Payload string // что придёт боту при нажатии
	OpenApp string // адрес мини-приложения: кнопка открывает его внутри MAX (этап 4)
}

// File — файл-вложение (отчёт .txt).
type File struct {
	Name    string
	Content []byte
}

// Message — одно сообщение бота. Разметка текста — только **жирный**:
// адаптер MAX превращает её в HTML, консоль печатает как есть.
type Message struct {
	Text    string
	Buttons [][]Button // ряды кнопок; у нас — по одной кнопке в ряд
	File    *File
}

// row — ряд из одной кнопки (в MAX длинные подписи в одном ряду обрезаются).
func row(text, payload string) []Button { return []Button{{Text: text, Payload: payload}} }
