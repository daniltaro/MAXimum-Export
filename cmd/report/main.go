// Команда report печатает отчёт бота в консоль — удобно проверять ядро без мессенджера.
//
//	go run ./cmd/report                                   # сценарий 1: сахар-песок в Китай
//	go run ./cmd/report -country am -code 0409000000 -qty 200 -weight 4000
//	go run ./cmd/report -country cn -code 1205109000 -qty 100 -weight 100000 -txt
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"maxexport/data"
	"maxexport/internal/engine"
	"maxexport/internal/report"
)

func main() {
	country := flag.String("country", "cn", "страна: cn, am, kz")
	code := flag.String("code", "1701121000", "код ТН ВЭД (10 цифр)")
	qty := flag.Int64("qty", 5000, "количество единиц")
	weight := flag.Float64("weight", 250000, "вес брутто, кг (0 — не указан)")
	net := flag.Float64("net", 0, "вес нетто, кг (необязательно)")
	ship := flag.String("ship", "", "дата отгрузки ДД.ММ.ГГГГ (необязательно)")
	txt := flag.Bool("txt", false, "напечатать текстовый документ .txt вместо сообщений чата")
	flag.Parse()

	e := engine.New(data.MustLoad())
	now := time.Now()
	if c := e.CheckCode(*code, now); c.Status != engine.CodeOK {
		fmt.Fprintf(os.Stderr, "код %s: не найден, не пищевой или устарел (статус %d)\n", *code, c.Status)
		os.Exit(1)
	}
	in := engine.Input{Country: *country, Code: engine.CodeDigits(*code), Qty: *qty, WeightKg: *weight, NetKg: *net}
	if *ship != "" {
		d, ok := engine.ParseDate(*ship, now)
		if !ok {
			fmt.Fprintln(os.Stderr, "дата отгрузки: формат ДД.ММ.ГГГГ")
			os.Exit(1)
		}
		in.ShipDate = d
	}
	if e.Cat.Country(in.Country) == nil {
		fmt.Fprintln(os.Stderr, "страна: cn, am или kz")
		os.Exit(1)
	}

	rep := report.Build(e.Calculate(in, engine.Env{Now: now, Rates: engine.Training(now)}))
	if *txt {
		fmt.Print(rep.Text())
		return
	}
	for i, part := range rep.ChatParts(report.ChatLimit) {
		fmt.Printf("────────── сообщение %d ──────────\n%s\n\n", i+1, part)
	}
}
