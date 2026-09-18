package optimizer

import (
	"encoding/json"
	"fmt"
	"math"
)

const Tolerance = 0.01

type HourInput struct {
	Demand float64
	Solar  float64
	Tariff float64
}

type BatteryConfig struct {
	Capacity     float64
	Initial      float64
	Minimum      float64
	MaxCharge    float64
	MaxDischarge float64
}

type Constraints struct {
	EffectiveSolar [24]float64
	ReserveFloor   [24]float64
	NoCharge       [24]bool
	NoDischarge    [24]bool
	GridCap        [24]float64
}

type Directive struct {
	Applies bool
	Type    string
	Hours   []int

	Factor           float64
	MinimumEnergyKwh float64
	MaxGridKwh       float64
}

const (
	DSolarReduction        = "solar_reduction"
	DMinimumBatteryReserve = "minimum_battery_reserve"
	DNoChargeWindow        = "no_charge_window"
	DNoDischargeWindow     = "no_discharge_window"
	DMaxGridWindow         = "max_grid_window"
	DNoOp                  = "no_op"
)

func DecodeDirective(directiveType string, applies bool, raw json.RawMessage) Directive {
	if directiveType == DNoOp || !applies {
		return Directive{Type: DNoOp}
	}
	var adj struct {
		Hours            []int    `json:"hours"`
		Factor           *float64 `json:"factor"`
		MinimumEnergyKwh *float64 `json:"minimum_energy_kwh"`
		MaxGridKwh       *float64 `json:"max_grid_kwh"`
	}
	if err := json.Unmarshal(raw, &adj); err != nil {
		return Directive{Type: DNoOp}
	}
	hours := cleanHours(adj.Hours)
	if len(hours) == 0 {
		return Directive{Type: DNoOp}
	}
	switch directiveType {
	case DSolarReduction:
		if adj.Factor == nil || math.IsNaN(*adj.Factor) || math.IsInf(*adj.Factor, 0) ||
			*adj.Factor < 0 || *adj.Factor > 1 {
			return Directive{Type: DNoOp}
		}
		return Directive{Type: directiveType, Applies: true, Hours: hours, Factor: *adj.Factor}
	case DMinimumBatteryReserve:
		if adj.MinimumEnergyKwh == nil || math.IsNaN(*adj.MinimumEnergyKwh) ||
			math.IsInf(*adj.MinimumEnergyKwh, 0) || *adj.MinimumEnergyKwh < 0 {
			return Directive{Type: DNoOp}
		}
		return Directive{Type: directiveType, Applies: true, Hours: hours, MinimumEnergyKwh: *adj.MinimumEnergyKwh}
	case DNoChargeWindow, DNoDischargeWindow:
		return Directive{Type: directiveType, Applies: true, Hours: hours}
	case DMaxGridWindow:
		if adj.MaxGridKwh == nil || math.IsNaN(*adj.MaxGridKwh) ||
			math.IsInf(*adj.MaxGridKwh, 0) || *adj.MaxGridKwh < 0 {
			return Directive{Type: DNoOp}
		}
		return Directive{Type: directiveType, Applies: true, Hours: hours, MaxGridKwh: *adj.MaxGridKwh}
	default:
		return Directive{Type: DNoOp}
	}
}

func cleanHours(in []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, h := range in {
		if h < 0 || h > 23 || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func BuildConstraints(hours []HourInput, batt BatteryConfig, dirs []Directive) Constraints {
	var c Constraints
	for h := 0; h < 24; h++ {
		c.EffectiveSolar[h] = hours[h].Solar
		c.ReserveFloor[h] = batt.Minimum
		c.GridCap[h] = math.Inf(1)
	}
	for _, d := range dirs {
		if !d.Applies || d.Type == DNoOp {
			continue
		}
		switch d.Type {
		case DSolarReduction:
			for _, h := range d.Hours {
				c.EffectiveSolar[h] = hours[h].Solar * d.Factor
			}
		case DMinimumBatteryReserve:
			for _, h := range d.Hours {
				if d.MinimumEnergyKwh > c.ReserveFloor[h] {
					c.ReserveFloor[h] = d.MinimumEnergyKwh
				}
			}
		case DNoChargeWindow:
			for _, h := range d.Hours {
				c.NoCharge[h] = true
			}
		case DNoDischargeWindow:
			for _, h := range d.Hours {
				c.NoDischarge[h] = true
			}
		case DMaxGridWindow:
			for _, h := range d.Hours {
				if d.MaxGridKwh < c.GridCap[h] {
					c.GridCap[h] = d.MaxGridKwh
				}
			}
		}
	}
	return c
}

type PlanEntry struct {
	Hour                  int
	GridKwh               float64
	SolarUsedKwh          float64
	BatteryAction         string
	BatteryKwh            float64
	BatteryEnergyAfterKwh float64
}

func stepFor(initial float64) float64 {
	if math.Abs(initial/0.5-math.Round(initial/0.5)) < 1e-9 {
		return 0.5
	}
	return 0.25
}

func Optimize(hours []HourInput, batt BatteryConfig, con Constraints) ([]PlanEntry, error) {
	s := stepFor(batt.Initial)
	n := int(math.Round(batt.Capacity / s))
	e0 := int(math.Round(batt.Initial / s))
	if e0 < 0 {
		e0 = 0
	}
	if e0 > n {
		e0 = n
	}

	snapErr := float64(e0)*s - batt.Initial

	const INF = 1e18
	prev := make([]float64, n+1)
	for i := range prev {
		prev[i] = INF
	}
	prev[e0] = 0

	type parent struct {
		prevIdx int
		charge  int
		dis     int
	}
	parents := make([][]parent, 24)
	for h := range parents {
		parents[h] = make([]parent, n+1)
		for i := range parents[h] {
			parents[h][i].prevIdx = -1
		}
	}

	maxChargeSteps := func(rem float64) int {
		m := math.Min(batt.MaxCharge, rem)
		return int(math.Floor(m/s + 1e-9))
	}

	for h := 0; h < 24; h++ {
		cur := make([]float64, n+1)
		for i := range cur {
			cur[i] = INF
		}
		for i := 0; i <= n; i++ {
			base := prev[i]
			if base >= INF/2 {
				continue
			}
			E := float64(i) * s

			relax := func(cSteps, dSteps int) {
				j := i + cSteps - dSteps
				if j < 0 || j > n {
					return
				}
				chg := float64(cSteps) * s
				dch := float64(dSteps) * s
				need := hours[h].Demand + chg - dch
				if need < -1e-9 {
					return
				}
				if need < 0 {
					need = 0
				}
				solarUsed := math.Min(con.EffectiveSolar[h], need)
				grid := need - solarUsed
				eAfter := E + chg - dch
				if eAfter < con.ReserveFloor[h]-Tolerance || eAfter > batt.Capacity+Tolerance {
					return
				}
				if grid > con.GridCap[h]+Tolerance {
					return
				}
				cost := base + grid*hours[h].Tariff
				if cost < cur[j]-1e-12 {
					cur[j] = cost
					parents[h][j] = parent{prevIdx: i, charge: cSteps, dis: dSteps}
				}
			}
			relax(0, 0)
			if !con.NoCharge[h] {
				for k := 1; k <= maxChargeSteps(batt.Capacity-E); k++ {
					relax(k, 0)
				}
			}
			if !con.NoDischarge[h] {
				maxD := math.Min(batt.MaxDischarge, E)
				for k := 1; k <= int(math.Floor(maxD/s+1e-9)); k++ {
					relax(0, k)
				}
			}
		}
		prev = cur
	}

	if prev[e0] >= INF/2 {
		return nil, fmt.Errorf("infeasible scenario under hard directives")
	}

	type act struct {
		c, d int
	}
	acts := make([]act, 24)
	idx := e0
	for h := 23; h >= 0; h-- {
		p := parents[h][idx]
		if p.prevIdx < 0 {
			return nil, fmt.Errorf("reconstruction failed at hour %d", h)
		}
		acts[h] = act{c: p.charge, d: p.dis}
		idx = p.prevIdx
	}

	plan := make([]PlanEntry, 24)

	E := batt.Initial
	for h := 0; h < 24; h++ {
		c := float64(acts[h].c) * s
		d := float64(acts[h].d) * s
		need := hours[h].Demand + c - d
		if need < 0 {
			need = 0
		}
		solarUsed := math.Min(con.EffectiveSolar[h], need)
		grid := need - solarUsed
		eAfter := E + c - d
		action := "idle"
		kwh := 0.0
		if acts[h].c > 0 {
			action, kwh = "charge", c
		} else if acts[h].d > 0 {
			action, kwh = "discharge", d
		}
		plan[h] = PlanEntry{
			Hour: h, GridKwh: grid, SolarUsedKwh: solarUsed,
			BatteryAction: action, BatteryKwh: kwh, BatteryEnergyAfterKwh: eAfter,
		}
		E = eAfter
	}
	if snapErr != 0 {

		for h, p := range plan {
			if p.BatteryEnergyAfterKwh < con.ReserveFloor[h]-Tolerance ||
				p.BatteryEnergyAfterKwh > batt.Capacity+Tolerance {
				return nil, fmt.Errorf("snap-shift bound violation at hour %d", h)
			}
		}
	}
	return plan, nil
}

func Totals(plan []PlanEntry, hours []HourInput) (grid, cost, peak float64) {
	for h, p := range plan {
		grid += p.GridKwh
		cost += p.GridKwh * hours[h].Tariff
		if p.GridKwh > peak {
			peak = p.GridKwh
		}
	}
	return grid, cost, peak
}

func Replay(plan []PlanEntry, hours []HourInput, batt BatteryConfig, c Constraints) error {
	if len(plan) != 24 {
		return fmt.Errorf("hourly_plan must contain 24 entries")
	}
	seen := map[int]bool{}
	E := batt.Initial
	for h, p := range plan {
		if p.Hour != h {
			return fmt.Errorf("hour %d out of order", h)
		}
		if seen[p.Hour] {
			return fmt.Errorf("duplicate hour %d", p.Hour)
		}
		seen[p.Hour] = true
		if !isFiniteNonNeg(p.GridKwh) || !isFiniteNonNeg(p.SolarUsedKwh) ||
			!isFiniteNonNeg(p.BatteryKwh) || math.IsNaN(p.BatteryEnergyAfterKwh) ||
			math.IsInf(p.BatteryEnergyAfterKwh, 0) {
			return fmt.Errorf("non-finite or negative value at hour %d", h)
		}
		var charge, dis float64
		switch p.BatteryAction {
		case "idle":
			if math.Abs(p.BatteryKwh) > Tolerance {
				return fmt.Errorf("idle must have battery_kwh 0 at hour %d", h)
			}
		case "charge":
			charge = p.BatteryKwh
			if charge > batt.MaxCharge+Tolerance {
				return fmt.Errorf("charge rate violation at hour %d", h)
			}
			if c.NoCharge[h] && charge > Tolerance {
				return fmt.Errorf("charging in no-charge window at hour %d", h)
			}
		case "discharge":
			dis = p.BatteryKwh
			if dis > batt.MaxDischarge+Tolerance {
				return fmt.Errorf("discharge rate violation at hour %d", h)
			}
			if c.NoDischarge[h] && dis > Tolerance {
				return fmt.Errorf("discharging in no-discharge window at hour %d", h)
			}
		default:
			return fmt.Errorf("invalid battery_action at hour %d", h)
		}
		eAfter := E + charge - dis
		if math.Abs(eAfter-p.BatteryEnergyAfterKwh) > Tolerance {
			return fmt.Errorf("battery transition mismatch at hour %d", h)
		}
		if eAfter < c.ReserveFloor[h]-Tolerance || eAfter > batt.Capacity+Tolerance {
			return fmt.Errorf("battery bound violation at hour %d", h)
		}
		if p.SolarUsedKwh > c.EffectiveSolar[h]+Tolerance {
			return fmt.Errorf("solar overuse at hour %d", h)
		}
		lhs := p.GridKwh + p.SolarUsedKwh + dis
		rhs := hours[h].Demand + charge
		if math.Abs(lhs-rhs) > Tolerance {
			return fmt.Errorf("energy balance failure at hour %d", h)
		}
		if p.GridKwh > c.GridCap[h]+Tolerance {
			return fmt.Errorf("grid cap violation at hour %d", h)
		}
		E = eAfter
	}
	if math.Abs(E-batt.Initial) > Tolerance {
		return fmt.Errorf("end-of-day battery neutrality violated")
	}
	return nil
}

func isFiniteNonNeg(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= -1e-9
}
