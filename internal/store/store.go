// Package store хранит выполненные расчёты в памяти процесса, чтобы к ним можно было
// вернуться: скачать .txt, задать вопрос, показать «С возвращением!».
//
// По ТЗ §19 базы данных нет: после перезапуска программы расчёты теряются.
// Старые расчёты удаляются через TTL, общее число ограничено — память не растёт бесконечно.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"maxexport/internal/engine"
	"maxexport/internal/report"
)

// Calc — сохранённый расчёт.
type Calc struct {
	ID      string
	Created time.Time
	Result  engine.Result
	Report  report.Report
}

// Store — потокобезопасное хранилище расчётов.
type Store struct {
	mu    sync.Mutex
	items map[string]*Calc
	order []string // порядок добавления — чтобы удалять самые старые
	ttl   time.Duration
	max   int
}

// New создаёт хранилище: расчёты живут ttl, одновременно хранится не больше max штук.
func New(ttl time.Duration, max int) *Store {
	return &Store{items: map[string]*Calc{}, ttl: ttl, max: max}
}

// Put сохраняет расчёт и возвращает его с присвоенным идентификатором.
func (s *Store) Put(r engine.Result, rep report.Report) *Calc {
	c := &Calc{ID: newID(), Created: r.At, Result: r, Report: rep}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(r.At)
	s.items[c.ID] = c
	s.order = append(s.order, c.ID)
	for len(s.order) > s.max {
		delete(s.items, s.order[0])
		s.order = s.order[1:]
	}
	return c
}

// Get возвращает расчёт по идентификатору (nil — не найден или устарел).
func (s *Store) Get(id string, now time.Time) *Calc {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.items[id]
	if c == nil || now.Sub(c.Created) > s.ttl {
		return nil
	}
	return c
}

// cleanup удаляет устаревшие расчёты (вызывается под блокировкой).
func (s *Store) cleanup(now time.Time) {
	keep := s.order[:0]
	for _, id := range s.order {
		if c := s.items[id]; c != nil && now.Sub(c.Created) <= s.ttl {
			keep = append(keep, id)
		} else {
			delete(s.items, id)
		}
	}
	s.order = keep
}

// newID — случайный идентификатор из 16 шестнадцатеричных символов.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
