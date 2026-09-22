package flow

import (
	"sync"
	"time"

	"maxexport/internal/service"
)

// screen — экран, на котором сейчас пользователь.
type screen int

const (
	scrStart          screen = iota // Экран 1
	scrCountry                      // Экран 2 · Шаг 1 из 4
	scrCountryConfirm               // «Вы указали "КНР". Это Китай?»
	scrProduct                      // Экран 3 · Шаг 2 из 4: поиск по названию
	scrCodeManual                   // Экран 3: ручной ввод кода
	scrQty                          // Экран 4 · Шаг 3 из 4: количество
	scrWeight                       // Экран 4 · Шаг 3 из 4: вес
	scrWeightCheck                  // нетипичный вес единицы: «кг или тонны?»
	scrReview                       // Шаг 4 из 4: «Проверьте данные»
	scrDate                         // ввод даты отгрузки
	scrEdit                         // «Что изменить?»
	scrResult                       // Экран 5
	scrAlt                          // «Альтернативные рынки»
	scrHelp                         // Экран 6: справка
	scrAsk                          // Экран 6: вопрос по расчёту
	scrDemo                         // режим ведущего
)

// draft — данные, которые пользователь ввёл на шагах 1–4.
type draft struct {
	Country      string
	Query        string   // что искал по названию (для проверки «код ↔ тип продукции»)
	Code         string   // выбранный код ТН ВЭД, 10 цифр
	ManualCode   bool     // код введён вручную
	ReplacedFrom string   // пользователь ввёл устаревший код — расчёт по новому
	Candidates   []string // коды, показанные списком (кнопки «1. …», «2. …»)

	Qty       int64
	UnitWord  string  // «мешков», если пользователь написал слово
	PerUnitKg float64 // «по 50 кг» из ввода количества

	WeightKg            float64 // брутто; 0 — вес пропущен
	NetKg               float64
	WeightDone          bool // вес введён или пропущен
	WeightExplicit      bool // единица указана явно («кг», «т»)
	UnitWeightConfirmed bool // пользователь подтвердил нетипичный вес единицы

	ShipDate time.Time // необязательная дата отгрузки
}

// Session — состояние одного пользователя. Хранится в памяти (ТЗ §19: без базы данных).
type Session struct {
	mu sync.Mutex

	screen screen
	view   int // номер показа экрана: зашит в кнопки, чтобы распознавать устаревшие
	d      draft

	pendingCountry string  // страна, которую нужно подтвердить
	pendingRawKg   float64 // вес, про который спросили «кг или тонны?»
	pendingNetKg   float64

	editing  bool   // пришли из «✏️ Изменить»: после шага — снова «Проверьте данные»
	returnTo screen // куда вернуться из справки
	calcID   string // расчёт, который сейчас на экране (результат, вопрос)
	lastID   string // последний расчёт пользователя — для «С возвращением!»
	demo     service.Demo

	touched time.Time
}

// sessions — все сессии; старые удаляются через TTL, их число ограничено.
type sessions struct {
	mu    sync.Mutex
	items map[string]*Session
	ttl   time.Duration
	max   int
}

func newSessions(ttl time.Duration, max int) *sessions {
	return &sessions{items: map[string]*Session{}, ttl: ttl, max: max}
}

// get возвращает сессию пользователя (новую, если её не было или она устарела).
func (ss *sessions) get(user string, now time.Time) *Session {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	s := ss.items[user]
	if s == nil || now.Sub(s.touched) > ss.ttl {
		if len(ss.items) >= ss.max {
			ss.evict(now)
		}
		s = &Session{}
		ss.items[user] = s
	}
	s.touched = now
	return s
}

// evict удаляет устаревшие сессии, а если их нет — самую старую (вызывается под блокировкой).
func (ss *sessions) evict(now time.Time) {
	oldest, oldestAt := "", now
	for k, s := range ss.items {
		if now.Sub(s.touched) > ss.ttl {
			delete(ss.items, k)
			continue
		}
		if s.touched.Before(oldestAt) {
			oldest, oldestAt = k, s.touched
		}
	}
	if len(ss.items) >= ss.max && oldest != "" {
		delete(ss.items, oldest)
	}
}
