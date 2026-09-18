package optimizer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSamplePackOptimizesReferenceDirectives(t *testing.T) {
	packPath := filepath.Join("..", "..", "instructions",
		"BUP_CSE_FEST_2026_Preli_Public_Sample_Cases.json")
	raw, err := os.ReadFile(packPath)
	if err != nil {
		t.Skipf("sample pack not found: %v", err)
	}
	var pack struct {
		Cases []struct {
			ID    string `json:"id"`
			Input struct {
				ScenarioID string `json:"scenario_id"`
				Hours      []struct {
					Hour   int     `json:"hour"`
					Demand float64 `json:"demand_kwh"`
					Solar  float64 `json:"solar_kwh"`
					Tariff float64 `json:"tariff_bdt_per_kwh"`
				} `json:"hours"`
				Battery struct {
					Capacity     float64 `json:"capacity_kwh"`
					Initial      float64 `json:"initial_energy_kwh"`
					Minimum      float64 `json:"minimum_energy_kwh"`
					MaxCharge    float64 `json:"max_charge_kwh_per_hour"`
					MaxDischarge float64 `json:"max_discharge_kwh_per_hour"`
				} `json:"battery"`
			} `json:"input"`
			Expected struct {
				Interpretation []struct {
					Applies    bool            `json:"applies"`
					Type       string          `json:"directive_type"`
					Adjustment json.RawMessage `json:"structured_adjustment"`
				} `json:"directive_interpretation"`
				TotalCost float64 `json:"total_cost_bdt"`
			} `json:"expected_output"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &pack); err != nil {
		t.Fatalf("parse sample pack: %v", err)
	}
	if len(pack.Cases) != 10 {
		t.Fatalf("expected 10 cases, got %d", len(pack.Cases))
	}
	for _, c := range pack.Cases {
		hours := make([]HourInput, 24)
		for _, h := range c.Input.Hours {
			hours[h.Hour] = HourInput{Demand: h.Demand, Solar: h.Solar, Tariff: h.Tariff}
		}
		batt := BatteryConfig{
			Capacity: c.Input.Battery.Capacity, Initial: c.Input.Battery.Initial,
			Minimum: c.Input.Battery.Minimum, MaxCharge: c.Input.Battery.MaxCharge,
			MaxDischarge: c.Input.Battery.MaxDischarge,
		}
		dirs := make([]Directive, len(c.Expected.Interpretation))
		for i, e := range c.Expected.Interpretation {
			dirs[i] = DecodeDirective(e.Type, e.Applies, e.Adjustment)
			if e.Applies && dirs[i].Type == DNoOp {
				t.Errorf("%s: reference directive %d failed to decode", c.ID, i)
			}
		}
		con := BuildConstraints(hours, batt, dirs)
		plan, err := Optimize(hours, batt, con)
		if err != nil {
			t.Errorf("%s: optimize: %v", c.ID, err)
			continue
		}
		if err := Replay(plan, hours, batt, con); err != nil {
			t.Errorf("%s: replay: %v", c.ID, err)
		}
		_, cost, _ := Totals(plan, hours)
		if diff := cost - c.Expected.TotalCost; diff < -Tolerance || diff > Tolerance {
			t.Errorf("%s: cost %.2f vs reference %.2f", c.ID, cost, c.Expected.TotalCost)
		}
	}
}
