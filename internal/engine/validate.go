package engine

import "maxexport/data"

// UnitCheck — проверка, сходятся ли между собой количество и вес партии.
type UnitCheck struct {
	PerUnitKg float64    // вес одной единицы при весе в килограммах
	Range     [2]float64 // типичный диапазон для товара, кг
	Atypical  bool       // вес единицы выходит за типичный диапазон

	// Может быть, вес указан в тоннах? Если при пересчёте «число × 1000 кг» вес единицы
	// становится типичным — бот спросит: «Вы указали 250 — это 250 кг или 250 тонн?»
	TonnesLikely    bool
	PerUnitIfTonnes float64
}

// CheckUnitWeight сравнивает вес одной единицы с типичным диапазоном для товара.
// explicitUnit — пользователь сам написал «кг» или «т», тогда вариант «тонны» не предлагаем.
func CheckUnitWeight(p *data.Product, qty int64, weightKg float64, explicitUnit bool) UnitCheck {
	c := UnitCheck{Range: p.UnitKg}
	if qty <= 0 || weightKg <= 0 || p.UnitKg[1] <= 0 {
		return c
	}
	c.PerUnitKg = weightKg / float64(qty)
	c.Atypical = !inRange(c.PerUnitKg, p.UnitKg)
	if c.Atypical && !explicitUnit {
		c.PerUnitIfTonnes = weightKg * 1000 / float64(qty)
		c.TonnesLikely = inRange(c.PerUnitIfTonnes, p.UnitKg)
	}
	return c
}

func inRange(v float64, r [2]float64) bool { return v >= r[0] && v <= r[1] }

// TarePct — доля тары в весе брутто, %.
func TarePct(grossKg, netKg float64) float64 {
	if grossKg <= 0 || netKg <= 0 {
		return 0
	}
	return (grossKg - netKg) / grossKg * 100
}

// Контейнерные перевозки: границы, по которым считаются число контейнеров и мелкая партия.
const (
	Container20MaxKg = 24000.0 // практическая загрузка 20-футового контейнера
	SmallBatchKg     = 100.0   // «мелкая партия»
)

// ContainersNeeded — сколько 20-футовых контейнеров нужно для партии.
func ContainersNeeded(kg float64) int {
	n := int(kg / Container20MaxKg)
	if float64(n)*Container20MaxKg < kg {
		n++
	}
	return n
}
