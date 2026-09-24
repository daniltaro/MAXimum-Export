package flow

import (
	"strconv"
	"strings"
)

// Что зашито в кнопки (payload).
//
// Кнопки шагов привязаны к показу экрана: «v12:country:cn» — нажатие засчитывается, только
// если пользователь всё ещё на показе №12. Под старыми сообщениями кнопки остаются, и без
// этой проверки кнопка «Китай» трёхшаговой давности сбила бы уже введённые данные.
//
// Кнопки результата и общие кнопки работают всегда: «g:copy:<id расчёта>» — скопировать
// именно тот расчёт, под которым нажали, даже если после него был другой.

// Действия шагов.
const (
	aBack      = "back"
	aCountry   = "country" // :cn
	aYes       = "yes"
	aNo        = "no"
	aCode      = "code" // :номер в списке
	aManual    = "manual"
	aSearch    = "search"
	aRetryCode = "retrycode"
	aRetry     = "retry"
	aSkipW     = "skipw"
	aSuggestW  = "sugw"
	aKg        = "kg"
	aTonnes    = "t"
	aWeightOK  = "wok"
	aWeightEd  = "wedit"
	aCalc      = "calc"
	aDate      = "date"
	aClearDate = "cleardate"
	aEdit      = "edit"
	aEditField = "ef" // :country | product | qty | weight | date
)

// Общие действия.
const (
	gNew     = "new"
	gStart   = "start"
	gHelp    = "help"
	gExample = "example"
	gRepeat  = "repeat"
	gDemo    = "demo"
	gDemoRun = "demorun" // :номер сценария
	gCopy    = "copy"    // :id
	gDl      = "dl"      // :id
	gEdit    = "edit"    // :id
	gAsk     = "ask"     // :id
	gAlt     = "alt"     // :id
	gAltCalc = "altcalc" // :id:страна
	gResult  = "result"  // :id — вернуться к результату
	gQ       = "q"       // :id:номер подсказки
)

// step — payload кнопки шага: привязан к текущему показу экрана.
func (s *Session) step(action string, args ...string) string {
	return "v" + strconv.Itoa(s.view) + ":" + join(action, args)
}

// global — payload общей кнопки.
func global(action string, args ...string) string { return "g:" + join(action, args) }

func join(action string, args []string) string {
	return strings.Join(append([]string{action}, args...), ":")
}

// parsed — разобранный payload.
type parsed struct {
	global bool
	view   int
	action string
	args   []string
}

func parsePayload(p string) (parsed, bool) {
	parts := strings.Split(p, ":")
	if len(parts) < 2 {
		return parsed{}, false
	}
	out := parsed{action: parts[1], args: parts[2:]}
	switch {
	case parts[0] == "g":
		out.global = true
	case strings.HasPrefix(parts[0], "v"):
		v, err := strconv.Atoi(parts[0][1:])
		if err != nil {
			return parsed{}, false
		}
		out.view = v
	default:
		return parsed{}, false
	}
	return out, true
}

func (p parsed) arg(i int) string {
	if i < len(p.args) {
		return p.args[i]
	}
	return ""
}
