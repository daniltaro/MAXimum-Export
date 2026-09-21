package engine

import (
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Курсы валют (ТЗ §13, §14.5, §14.8, §20.6)
// ---------------------------------------------------------------------------

// Источники курса.
const (
	SourceTraining = "training" // учебный курс: 1 € = 100 ₽ (ТЗ §20.6)
	SourceCBR      = "cbr"      // официальный курс ЦБ РФ (включается в .env: RATE_SOURCE=cbr)
)

// TrainingRates — учебные курсы. Значение EUR задано в ТЗ, остальные — условные.
var TrainingRates = map[string]float64{"EUR": 100, "USD": 90, "CNY": 12.5}

// Rates — снимок курсов на момент расчёта. Расчёт пошлины получает готовый снимок
// и в сеть не ходит — поэтому ответ бота всегда укладывается в 5 секунд (ТЗ §21).
type Rates struct {
	Values map[string]float64 // сколько рублей стоит 1 единица валюты: "EUR" → 100
	Date   time.Time          // на какую дату курс
	Source string             // SourceTraining или SourceCBR
	Failed bool               // свежий курс получить не удалось — используется последний известный (ТЗ §14.8)
}

// Get — курс валюты в рублях. Для рублей всегда 1.
func (r Rates) Get(currency string) (float64, bool) {
	if currency == "" || currency == "RUB" {
		return 1, true
	}
	v, ok := r.Values[currency]
	return v, ok
}

// Training — снимок учебных курсов на дату now.
func Training(now time.Time) Rates {
	return Rates{Values: TrainingRates, Date: Day(now), Source: SourceTraining}
}

// RateSource — откуда бот берёт курсы.
type RateSource interface {
	Current(now time.Time) Rates
}

// TrainingSource — всегда учебный курс (режим по умолчанию).
type TrainingSource struct{}

func (TrainingSource) Current(now time.Time) Rates { return Training(now) }

// ---------------------------------------------------------------------------
// Курс ЦБ РФ: загружается в фоне раз в час, ответы бота берут последний сохранённый.
// ---------------------------------------------------------------------------

// CBRURL — официальный ежедневный курс ЦБ РФ (XML).
const CBRURL = "https://www.cbr.ru/scripts/XML_daily.asp"

// CBRSource хранит последний успешно загруженный курс ЦБ.
type CBRSource struct {
	URL    string
	Client *http.Client

	mu     sync.RWMutex
	last   Rates // последний успешный курс
	loaded bool  // был ли хоть один успешный запрос
	failed bool  // последний запрос завершился ошибкой
}

// NewCBRSource создаёт источник курса ЦБ с таймаутом запроса 3 секунды.
func NewCBRSource() *CBRSource {
	return &CBRSource{URL: CBRURL, Client: &http.Client{Timeout: 3 * time.Second}}
}

// Current возвращает последний известный курс. Если ЦБ ни разу не ответил —
// учебный курс с пометкой Failed, чтобы в отчёте появилось предупреждение.
func (s *CBRSource) Current(now time.Time) Rates {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.loaded {
		r := Training(now)
		r.Failed = true
		return r
	}
	r := s.last
	r.Failed = s.failed
	return r
}

// Run обновляет курс сразу и затем каждые every, пока не отменён ctx.
func (s *CBRSource) Run(ctx context.Context, every time.Duration) {
	for {
		if err := s.Refresh(ctx); err != nil {
			log.Printf("курс ЦБ: %v (используется последний известный)", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// Refresh загружает свежий курс ЦБ.
func (s *CBRSource) Refresh(ctx context.Context) error {
	r, err := s.fetch(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.failed = true
		return err
	}
	s.last, s.loaded, s.failed = r, true, false
	return nil
}

// cbrXML — структура ответа ЦБ: <ValCurs Date="21.09.2026"><Valute>...</Valute></ValCurs>
type cbrXML struct {
	Date    string `xml:"Date,attr"`
	Valutes []struct {
		CharCode string `xml:"CharCode"`
		Nominal  string `xml:"Nominal"`
		Value    string `xml:"Value"`
	} `xml:"Valute"`
}

func (s *CBRSource) fetch(ctx context.Context) (Rates, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return Rates{}, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return Rates{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Rates{}, fmt.Errorf("ЦБ ответил %s", resp.Status)
	}

	var doc cbrXML
	dec := xml.NewDecoder(resp.Body)
	dec.CharsetReader = func(charset string, in io.Reader) (io.Reader, error) {
		return cp1251Reader(in), nil // ЦБ отдаёт XML в кодировке windows-1251
	}
	if err := dec.Decode(&doc); err != nil {
		return Rates{}, fmt.Errorf("разбор XML ЦБ: %w", err)
	}

	r := Rates{Values: map[string]float64{}, Source: SourceCBR}
	if d, err := time.ParseInLocation("02.01.2006", doc.Date, MSK); err == nil {
		r.Date = d
	}
	for _, v := range doc.Valutes {
		val, err1 := strconv.ParseFloat(strings.ReplaceAll(v.Value, ",", "."), 64)
		nom, err2 := strconv.ParseFloat(v.Nominal, 64)
		if err1 == nil && err2 == nil && nom > 0 {
			r.Values[v.CharCode] = val / nom // курс за 1 единицу (у юаня номинал может быть 10)
		}
	}
	if _, ok := r.Values["EUR"]; !ok {
		return Rates{}, fmt.Errorf("в ответе ЦБ нет курса EUR")
	}
	return r, nil
}

// cp1251Reader перекодирует windows-1251 в UTF-8 (русские буквы; прочие символы → «?»).
func cp1251Reader(in io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		br := bufio.NewReader(in)
		bw := bufio.NewWriter(pw)
		for {
			b, err := br.ReadByte()
			if err != nil {
				bw.Flush()
				pw.CloseWithError(err)
				return
			}
			switch {
			case b < 0x80:
				bw.WriteByte(b)
			case b >= 0xC0:
				bw.WriteRune(rune(0x410 + int(b) - 0xC0)) // А..я
			case b == 0xA8:
				bw.WriteRune('Ё')
			case b == 0xB8:
				bw.WriteRune('ё')
			default:
				bw.WriteByte('?')
			}
		}
	}()
	return pr
}
