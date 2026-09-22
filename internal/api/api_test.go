package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/service"
)

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, engine.MSK)

func newServer(t *testing.T) http.Handler {
	t.Helper()
	svc := service.New(data.MustLoad(), engine.TrainingSource{})
	svc.Now = func() time.Time { return now }
	return New(svc, Options{AllowedOrigins: "*", RatePerMinute: 1000}).Handler()
}

func do(t *testing.T, h http.Handler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if s, ok := body.(string); ok {
			buf.WriteString(s)
		} else {
			_ = json.NewEncoder(&buf).Encode(body)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// Сценарий 1 (ТЗ §22) через API мини-приложения: поиск → расчёт → .txt → вопросы тренера.
func TestScenario1ThroughAPI(t *testing.T) {
	h := newServer(t)

	rec, _ := do(t, h, "GET", "/api/v1/products/search?q=сахар-песок", nil)
	var search SearchDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &search)
	if rec.Code != 200 || len(search.Items) == 0 || search.Items[0].Code != "1701121000" {
		t.Fatalf("поиск: %d %s", rec.Code, rec.Body.String())
	}

	w := 250000.0
	rec, _ = do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "cn", Code: "1701 12 100 0", Quantity: 5000, UnitWord: "мешков", WeightKg: &w})
	if rec.Code != 201 {
		t.Fatalf("расчёт: %d %s", rec.Code, rec.Body.String())
	}
	var calc CalcDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &calc)
	if calc.Duty.Regime != "third" || calc.Duty.TotalRub != 0 || calc.Duty.FeeRub != 8262 {
		t.Errorf("пошлина: %+v", calc.Duty)
	}
	if len(calc.Roles) != 5 || calc.Disclaimer == "" || len(calc.Documents) == 0 || len(calc.ChatParts) == 0 {
		t.Errorf("неполный результат: роли %d, документы %d", len(calc.Roles), len(calc.Documents))
	}
	var ids []string
	for _, b := range calc.Blocks {
		ids = append(ids, b.ID)
	}
	if got := strings.Join(ids, ","); got != "params,packaging,product,labeling,documents,lab,duty,roles,warnings" {
		t.Errorf("порядок блоков: %s", got)
	}

	rec, _ = do(t, h, "GET", calc.DownloadURL, nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), calc.FileName) ||
		!strings.Contains(rec.Body.String(), "ПАРАМЕТРЫ СДЕЛКИ") {
		t.Errorf("скачивание .txt: %d %q", rec.Code, rec.Header().Get("Content-Disposition"))
	}

	// ТЗ §22, сценарий 1 — что проверяет тренер: «Какие сертификаты?», «Сколько пошлина?», «Сколько ждать регистрацию?»
	checks := map[string]string{
		"Какие сертификаты нужны?":   "GACC",
		"Сколько пошлина?":           "0 ₽",
		"Сколько ждать регистрацию?": "2–6",
	}
	for q, want := range checks {
		rec, out := do(t, h, "POST", "/api/v1/calculations/"+calc.ID+"/questions", map[string]string{"question": q})
		if rec.Code != 200 || out["found"] != true || !strings.Contains(out["text"].(string), want) {
			t.Errorf("вопрос %q: %d, ответ не содержит %q: %v", q, rec.Code, want, out["text"])
		}
	}
}

func TestScenario2And3ThroughAPI(t *testing.T) {
	h := newServer(t)
	w := 4000.0
	rec, _ := do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "am", Code: "0409000000", Quantity: 200, WeightKg: &w})
	var calc CalcDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &calc)
	if rec.Code != 201 || calc.Duty.Regime != "eaeu" || calc.Duty.FeeRub != 0 {
		t.Errorf("мёд в Армению: %d %+v", rec.Code, calc.Duty)
	}

	// Сценарий 3: сначала проверка веса 5 кг на 1000 мешков — нетипично, возможно тонны.
	rec, out := do(t, h, "POST", "/api/v1/checks/weight", WeightCheckRequest{Code: "1101000000", Quantity: 1000, WeightKg: 5})
	if rec.Code != 200 || out["atypical"] != true || out["tonnes_likely"] != true || !strings.Contains(out["message"].(string), "0,005") {
		t.Errorf("проверка веса: %d %v", rec.Code, out)
	}
	w = 50000
	rec, _ = do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "kz", Code: "1101 00 00 00", Quantity: 1000, WeightKg: &w})
	if rec.Code != 201 {
		t.Errorf("мука в Казахстан: %d %s", rec.Code, rec.Body.String())
	}
}

func TestValidationErrors(t *testing.T) {
	h := newServer(t)
	w := 100.0
	cases := []struct {
		name  string
		body  any
		field string
	}{
		{"страна не поддерживается", CalcRequestDTO{Country: "tr", Code: "1701121000", Quantity: 1, WeightKg: &w}, "country"},
		{"нулевое количество", CalcRequestDTO{Country: "cn", Code: "1701121000", Quantity: 0, WeightKg: &w}, "quantity"},
		{"отрицательный вес", map[string]any{"country": "cn", "code": "1701121000", "quantity": 1, "weight_kg": -5}, "weight_kg"},
		{"не пищевой код", CalcRequestDTO{Country: "cn", Code: "8471300000", Quantity: 1, WeightKg: &w}, "code"},
		{"несуществующий код", CalcRequestDTO{Country: "cn", Code: "9999999999", Quantity: 1, WeightKg: &w}, "code"},
		{"дата в прошлом", CalcRequestDTO{Country: "cn", Code: "1701121000", Quantity: 1, WeightKg: &w, ShipDate: "2026-01-01"}, "ship_date"},
	}
	for _, c := range cases {
		rec, out := do(t, h, "POST", "/api/v1/calculations", c.body)
		errObj, _ := out["error"].(map[string]any)
		if rec.Code != 400 || errObj["field"] != c.field || errObj["message"] == "" {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
	}

	// Неизвестное поле, битый JSON, слишком большой запрос.
	if rec, _ := do(t, h, "POST", "/api/v1/calculations", `{"country":"cn","inn":"7701234567"}`); rec.Code != 400 {
		t.Errorf("неизвестное поле должно отклоняться: %d", rec.Code)
	}
	if rec, _ := do(t, h, "POST", "/api/v1/calculations", `{bad`); rec.Code != 400 {
		t.Errorf("битый JSON: %d", rec.Code)
	}
	if rec, _ := do(t, h, "POST", "/api/v1/calculations", `{"code":"`+strings.Repeat("1", MaxBodyBytes)+`"}`); rec.Code != 413 {
		t.Errorf("большой запрос: %d", rec.Code)
	}
	if rec, _ := do(t, h, "GET", "/api/v1/calculations/unknown", nil); rec.Code != 404 {
		t.Errorf("неизвестный расчёт: %d", rec.Code)
	}
}

func TestQuestionGuards(t *testing.T) {
	h := newServer(t)
	w := 250000.0
	rec, _ := do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "cn", Code: "1701121000", Quantity: 5000, WeightKg: &w})
	var calc CalcDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &calc)
	path := "/api/v1/calculations/" + calc.ID + "/questions"

	if rec, _ := do(t, h, "POST", path, map[string]string{"question": "мой телефон +7 999 123-45-67"}); rec.Code != 400 {
		t.Errorf("вопрос с телефоном должен отклоняться: %d", rec.Code)
	}
	if rec, _ := do(t, h, "POST", path, map[string]string{"question": strings.Repeat("а", MaxQuestionLen+1)}); rec.Code != 400 {
		t.Errorf("слишком длинный вопрос: %d", rec.Code)
	}
	rec, out := do(t, h, "POST", path, map[string]string{"question": "абракадабра"})
	if rec.Code != 200 || out["found"] != false || len(out["suggestions"].([]any)) == 0 {
		t.Errorf("нераспознанный вопрос: %d %v", rec.Code, out)
	}
}

func TestCodeCheckStatuses(t *testing.T) {
	h := newServer(t)
	cases := map[string]string{
		"1701121000": "ok", "0403101100": "replaced", "8471300000": "non_food",
		"9999999999": "not_found", "1701": "prefix", "12": "bad_format",
	}
	for code, want := range cases {
		rec, out := do(t, h, "GET", "/api/v1/products/"+code, nil)
		if rec.Code != 200 || out["status"] != want {
			t.Errorf("код %s: статус %v, ожидался %s", code, out["status"], want)
		}
	}
}

func TestReferenceEndpoints(t *testing.T) {
	h := newServer(t)
	for _, path := range []string{"/healthz", "/api/v1/meta", "/api/v1/texts", "/api/v1/help", "/api/v1/countries", "/api/v1/openapi.yaml"} {
		if rec, _ := do(t, h, "GET", path, nil); rec.Code != 200 || rec.Body.Len() == 0 {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	// CORS: предварительный запрос браузера.
	req := httptest.NewRequest("OPTIONS", "/api/v1/calculations", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("CORS: %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

// Режим ведущего через API: сбой курса и скачок курса.
func TestDemoFlags(t *testing.T) {
	h := newServer(t)
	w := 100000.0
	rec, _ := do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "cn", Code: "1205109000", Quantity: 100, WeightKg: &w,
		Demo: &DemoDTO{RateFail: true, RateJumpPct: 5}})
	var calc CalcDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &calc)
	codes := map[string]bool{}
	for _, w := range calc.Warnings {
		codes[w.Code] = true
	}
	if !calc.Rates.Failed || !codes[engine.WRateFailed] || !codes[engine.WRateJump] {
		t.Errorf("демо-флаги: failed=%v, предупреждения %v", calc.Rates.Failed, codes)
	}
}

// Регрессионные тесты по итогам ревью этапа 1.5.
func TestReviewRegressions(t *testing.T) {
	h := newServer(t)

	// Очень длинный поисковый запрос отклоняется сразу (раньше — 8 с работы процессора).
	start := time.Now()
	if rec, _ := do(t, h, "GET", "/api/v1/products/search?q="+strings.Repeat("z", 5000), nil); rec.Code != 400 {
		t.Errorf("длинный запрос поиска: %d", rec.Code)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("отклонение длинного запроса заняло %v", d)
	}

	// Персональные данные в полях расчёта отклоняются с указанием поля и вида данных.
	w := 100.0
	rec, out := do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "cn", Code: "1701121000", Quantity: 1, WeightKg: &w,
		ProductQuery: "сахар, звоните +7 (999) 123-45-67"})
	errObj, _ := out["error"].(map[string]any)
	if rec.Code != 400 || errObj["code"] != "personal_data" || errObj["field"] != "product_query" || errObj["kind"] == "" {
		t.Errorf("ПДн в product_query: %d %v", rec.Code, out)
	}

	// Огромный вес — ошибка 400, а не «бесконечность» и пустой ответ.
	huge := 1e306
	if rec, _ := do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "cn", Code: "1205109000", Quantity: 1, WeightKg: &huge}); rec.Code != 400 {
		t.Errorf("вес 1e306: %d", rec.Code)
	}
	if rec, _ := do(t, h, "POST", "/api/v1/checks/weight", WeightCheckRequest{Code: "1101000000", Quantity: 1, WeightKg: 1e306}); rec.Code != 400 {
		t.Errorf("проверка веса 1e306: %d", rec.Code)
	}

	// Нетто без брутто — ошибка в поле weight_kg.
	net := 50.0
	rec, out = do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "cn", Code: "1701121000", Quantity: 1, NetKg: &net})
	errObj, _ = out["error"].(map[string]any)
	if rec.Code != 400 || errObj["field"] != "weight_kg" {
		t.Errorf("нетто без брутто: %d %v", rec.Code, out)
	}

	// Код, который начнёт действовать только в будущем.
	if _, out := do(t, h, "GET", "/api/v1/products/2101110019", nil); out["status"] != "not_yet_valid" {
		t.Errorf("будущий код: %v", out["status"])
	}

	// Запрет экспорта: в JSON нет пошлины, документов и ролей — как в чате.
	svc := service.New(data.MustLoad(), engine.TrainingSource{})
	rice := svc.Engine.Cat.ProductsWithPrefix("100610")[0]
	w = 20000
	rec, out = do(t, h, "POST", "/api/v1/calculations", CalcRequestDTO{Country: "cn", Code: rice.Code, Quantity: 20, WeightKg: &w})
	if rec.Code != 201 || out["stop"] == nil || out["duty"] != nil || len(out["documents"].([]any)) != 0 || len(out["roles"].([]any)) != 0 {
		t.Errorf("запрет экспорта: stop=%v duty=%v", out["stop"], out["duty"])
	}
}

func TestRateLimit(t *testing.T) {
	svc := service.New(data.MustLoad(), engine.TrainingSource{})
	svc.Now = func() time.Time { return now }
	h := New(svc, Options{AllowedOrigins: "*", RatePerMinute: 2}).Handler()
	w := 100.0
	body := CalcRequestDTO{Country: "cn", Code: "1701121000", Quantity: 2, WeightKg: &w}
	for i := 0; i < 2; i++ {
		if rec, _ := do(t, h, "POST", "/api/v1/calculations", body); rec.Code != 201 {
			t.Fatalf("запрос %d: %d", i+1, rec.Code)
		}
	}
	if rec, _ := do(t, h, "POST", "/api/v1/calculations", body); rec.Code != 429 {
		t.Errorf("третий запрос за минуту: %d, ожидалось 429", rec.Code)
	}
	if rec, _ := do(t, h, "GET", "/api/v1/countries", nil); rec.Code != 200 {
		t.Errorf("GET-запросы не ограничиваются: %d", rec.Code)
	}
}
