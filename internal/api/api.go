// Package api — HTTP API для мини-приложения MAX (JSON). Описание — openapi.yaml рядом
// (также доступно по адресу GET /api/v1/openapi.yaml), контракт экранов — docs/screens.md.
//
// API — тонкая обёртка над internal/service: вся логика там, здесь только разбор запроса,
// вызов сервиса и превращение ответа в JSON.
package api

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"maxexport/internal/engine"
	"maxexport/internal/faq"
	"maxexport/internal/report"
	"maxexport/internal/service"
	"maxexport/texts"
)

//go:embed openapi.yaml
var openapiYAML []byte

// MaxBodyBytes — максимальный размер тела запроса (защита публичного сервера).
const MaxBodyBytes = 64 << 10

// MaxQuestionLen — максимальная длина вопроса по отчёту.
const MaxQuestionLen = 500

// MaxFieldLen — максимальная длина текстовых полей (поиск, название товара, единица, код).
const MaxFieldLen = 200

// Server — обработчик HTTP-запросов API.
type Server struct {
	svc            *service.Service
	allowedOrigins []string // для CORS; "*" — любой источник
	limiter        *rateLimiter
	trustProxy     bool // брать адрес клиента из X-Forwarded-For (когда API стоит за Caddy/nginx)
}

// Options — настройки API.
type Options struct {
	AllowedOrigins string // адреса фронтенда через запятую или "*"
	TrustProxy     bool   // сервер работает за обратным прокси
	RatePerMinute  int    // сколько POST-запросов в минуту разрешено одному адресу (0 — 60)
}

// New создаёт API.
func New(svc *service.Service, opt Options) *Server {
	var origins []string
	for _, o := range strings.Split(opt.AllowedOrigins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	if opt.RatePerMinute <= 0 {
		opt.RatePerMinute = 60
	}
	return &Server{svc: svc, allowedOrigins: origins, trustProxy: opt.TrustProxy, limiter: newRateLimiter(opt.RatePerMinute, time.Minute)}
}

// Handler возвращает http.Handler со всеми маршрутами /api/v1/... и /healthz.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /api/v1/meta", s.meta)
	mux.HandleFunc("GET /api/v1/texts", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, texts.All()) })
	mux.HandleFunc("GET /api/v1/help", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, faq.Help()) })
	mux.HandleFunc("GET /api/v1/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(openapiYAML)
	})

	mux.HandleFunc("GET /api/v1/countries", s.countries)
	mux.HandleFunc("GET /api/v1/products/search", s.search)
	mux.HandleFunc("GET /api/v1/products/{code}", s.checkCode)
	mux.HandleFunc("POST /api/v1/checks/weight", s.checkWeight)

	mux.HandleFunc("POST /api/v1/calculations", s.calculate)
	mux.HandleFunc("GET /api/v1/calculations/{id}", s.getCalc)
	mux.HandleFunc("GET /api/v1/calculations/{id}/report.txt", s.reportTxt)
	mux.HandleFunc("POST /api/v1/calculations/{id}/questions", s.ask)

	return s.withMiddleware(mux)
}

// ---------------------------------------------------------------------------
// Обработчики
// ---------------------------------------------------------------------------

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	now := s.svc.Now()
	rates := s.svc.Rates.Current(now)
	asOf, _ := engine.ParseISODate(s.svc.Engine.Cat.Measures.DataAsOf)
	writeJSON(w, http.StatusOK, map[string]any{
		"bot_name":   texts.T("common.bot_name"),
		"data_as_of": engine.FormatDate(asOf),
		"rates":      RatesDTO{Source: rates.Source, Date: engine.FormatDate(rates.Date), Failed: rates.Failed, Values: rates.Values},
		"disclaimer": report.Disclaimer,
		"training":   true, // все данные учебные (ТЗ §21)
	})
}

func (s *Server) countries(w http.ResponseWriter, r *http.Request) {
	var out []CountryDTO
	for i := range s.svc.Countries() {
		out = append(out, countryDTO(&s.svc.Countries()[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "q", texts.T("product.prompt"))
		return
	}
	if !checkText(w, "q", q, MaxFieldLen) {
		return
	}
	res := s.svc.Search(q)
	out := SearchDTO{Query: q, Total: res.Total, TooMany: res.TooMany, Items: []ProductDTO{}}
	for _, p := range res.Items {
		out.Items = append(out.Items, productDTO(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) checkCode(w http.ResponseWriter, r *http.Request) {
	input := r.PathValue("code")
	if !checkText(w, "code", input, MaxFieldLen) {
		return
	}
	c := s.svc.CheckCode(input)
	out := CodeCheckDTO{}
	code := engine.FormatCode(c.Digits)
	switch c.Status {
	case engine.CodeOK:
		out.Status = "ok"
	case engine.CodeReplaced:
		out.Status = "replaced"
		old := productDTO(c.Old)
		out.ReplacedFrom = &old
		out.Message = texts.T("code.replaced", "old", engine.FormatCode(c.Old.Code), "new", engine.FormatCode(c.Product.Code),
			"date", isoToRu(c.Old.ReplacedBy.Since))
	case engine.CodeNonFood:
		out.Status, out.Message = "non_food", texts.T("code.non_food", "code", code, "category", c.Product.Category)
	case engine.CodeNotYetValid:
		out.Status, out.Message = "not_yet_valid", service.NotYetValidMessage(c)
	case engine.CodePrefix:
		out.Status = "prefix"
		for _, p := range c.Candidates {
			out.Candidates = append(out.Candidates, productDTO(p))
		}
	case engine.CodeNotFound:
		out.Status, out.Message = "not_found", texts.T("code.not_found", "code", code)
	default:
		out.Status, out.Message = "bad_format", texts.T("code.bad_format", "input", engine.ClipInput(input, service.MaxInputEcho))
	}
	if c.Product != nil {
		p := productDTO(c.Product)
		out.Product = &p
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) checkWeight(w http.ResponseWriter, r *http.Request) {
	var req WeightCheckRequest
	if !readJSON(w, r, &req) {
		return
	}
	uc, err := s.svc.CheckWeight(engine.CodeDigits(req.Code), req.Quantity, req.WeightKg, req.ExplicitUnit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	out := WeightCheckDTO{PerUnitKg: uc.PerUnitKg, TypicalUnitKg: uc.Range, Atypical: uc.Atypical,
		TonnesLikely: uc.TonnesLikely, PerUnitIfTonnes: uc.PerUnitIfTonnes}
	if uc.Atypical {
		p := s.svc.Engine.Cat.Product(engine.CodeDigits(req.Code))
		out.Message = texts.T("weight.atypical", "per_unit", engine.FormatNum(uc.PerUnitKg), "product", p.Name) + " " +
			texts.T("weight.atypical_range", "min", engine.FormatNum(uc.Range[0]), "max", engine.FormatNum(uc.Range[1]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) calculate(w http.ResponseWriter, r *http.Request) {
	var req CalcRequestDTO
	if !readJSON(w, r, &req) {
		return
	}
	for _, f := range []struct{ name, value string }{
		{"country", req.Country}, {"code", req.Code}, {"product_query", req.ProductQuery},
		{"unit_word", req.UnitWord}, {"previous_id", req.PreviousID},
	} {
		if !checkText(w, f.name, f.value, MaxFieldLen) {
			return
		}
	}
	creq := service.CalcRequest{
		Country: req.Country, Code: req.Code,
		ProductQuery: req.ProductQuery, ManualCode: req.ManualCode, Quantity: req.Quantity, UnitWord: req.UnitWord,
		UnitWeightConfirmed: req.UnitWeightConfirmed, PreviousID: req.PreviousID,
	}
	if req.WeightKg != nil {
		if err := engine.CheckWeight(*req.WeightKg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", "weight_kg", err.Error())
			return
		}
		creq.WeightKg = *req.WeightKg
	}
	if req.NetKg != nil {
		if err := engine.CheckWeight(*req.NetKg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", "net_kg", err.Error())
			return
		}
		creq.NetKg = *req.NetKg
	}
	if req.ShipDate != "" {
		d, ok := engine.ParseISODate(req.ShipDate)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_input", "ship_date", texts.T("error.bad_iso_date"))
			return
		}
		creq.ShipDate = d
	}
	if req.Demo != nil {
		creq.Demo = service.Demo{RateFail: req.Demo.RateFail, RateJumpPct: req.Demo.RateJumpPct,
			ProfileDown: req.Demo.ProfileDown, DataStale: req.Demo.DataStale}
	}
	c, err := s.svc.Calculate(creq)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, calcDTO(c))
}

func (s *Server) getCalc(w http.ResponseWriter, r *http.Request) {
	c, err := s.svc.Get(r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, calcDTO(c))
}

func (s *Server) reportTxt(w http.ResponseWriter, r *http.Request) {
	c, err := s.svc.Get(r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+c.Report.FileName()+`"`)
	_, _ = w.Write([]byte(c.Report.Text()))
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string `json:"question"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	q := strings.TrimSpace(req.Question)
	switch {
	case q == "":
		writeError(w, http.StatusBadRequest, "invalid_input", "question", texts.T("ask.empty"))
		return
	case len([]rune(q)) > MaxQuestionLen:
		writeError(w, http.StatusBadRequest, "invalid_input", "question", texts.T("ask.too_long", "limit", engine.FormatInt(MaxQuestionLen)))
		return
	}
	if !checkText(w, "question", q, MaxQuestionLen) {
		return
	}
	ans, err := s.svc.Ask(r.PathValue("id"), q)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ans)
}

// ---------------------------------------------------------------------------
// Вспомогательные функции
// ---------------------------------------------------------------------------

// writeJSON сначала целиком кодирует ответ и только потом отправляет заголовки:
// если закодировать не удалось, клиент получит 500 с понятной ошибкой, а не пустое тело.
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("api: не удалось закодировать ответ: %v", err)
		buf.Reset()
		status = http.StatusInternalServerError
		_ = json.NewEncoder(&buf).Encode(ErrorDTO{Error: ErrorBody{Code: "internal", Message: texts.T("error.generic")}})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, status int, code, field, msg string) {
	writeJSON(w, status, ErrorDTO{Error: ErrorBody{Code: code, Field: field, Message: msg}})
}

// checkText проверяет текстовое поле: длина и отсутствие персональных данных (ТЗ §17).
// При ошибке сам отвечает клиенту и возвращает false.
func checkText(w http.ResponseWriter, field, value string, limit int) bool {
	if len([]rune(value)) > limit {
		writeError(w, http.StatusBadRequest, "invalid_input", field, texts.T("error.too_long", "limit", engine.FormatInt(int64(limit))))
		return false
	}
	if kind := engine.DetectPII(value); kind != "" {
		writeJSON(w, http.StatusBadRequest, ErrorDTO{Error: ErrorBody{Code: "personal_data", Field: field, Kind: kind,
			Message: texts.T("error.pii", "kind", kind)}})
		return false
	}
	return true
}

func writeServiceError(w http.ResponseWriter, err error) {
	var ie *service.InputError
	switch {
	case errors.As(err, &ie):
		writeError(w, http.StatusBadRequest, "invalid_input", ie.Field, ie.Message)
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "", err.Error())
	default:
		log.Printf("api: внутренняя ошибка: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "", texts.T("error.generic"))
	}
}

// readJSON читает тело запроса в v. При ошибке сам отвечает клиенту и возвращает false.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "", texts.T("error.body_too_large"))
		} else {
			writeError(w, http.StatusBadRequest, "bad_json", "", "Некорректный JSON: "+err.Error())
		}
		return false
	}
	return true
}

func isoToRu(s string) string {
	if t, ok := engine.ParseISODate(s); ok {
		return engine.FormatDate(t)
	}
	return s
}

// withMiddleware добавляет CORS, перехват паник и журнал запросов (без тела — чтобы не
// писать в логи пользовательский ввод).
func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		if origin := r.Header.Get("Origin"); origin != "" && s.originAllowed(origin) {
			sw.Header().Set("Access-Control-Allow-Origin", origin)
			sw.Header().Set("Vary", "Origin")
			sw.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			sw.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			sw.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPost && !s.limiter.Allow(s.clientIP(r), start) {
			sw.Header().Set("Retry-After", "60")
			writeError(sw, http.StatusTooManyRequests, "rate_limited", "", texts.T("error.rate_limit"))
			return
		}

		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("api: паника при %s %s: %v", r.Method, r.Pattern, rec)
				writeError(sw, http.StatusInternalServerError, "internal", "", texts.T("error.generic"))
			}
			// В журнал пишем шаблон маршрута («GET /api/v1/products/{code}»), а не сам адрес:
			// в адресе может оказаться то, что ввёл пользователь.
			if r.URL.Path != "/healthz" {
				pattern := r.Pattern
				if pattern == "" {
					pattern = r.Method + " (неизвестный адрес)"
				}
				log.Printf("%s → %d за %v", pattern, sw.status, time.Since(start).Round(time.Millisecond))
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.allowedOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
