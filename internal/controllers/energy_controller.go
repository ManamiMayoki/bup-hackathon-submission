package controllers

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"gridwise/internal/optimizer"
	"gridwise/internal/services"
)

func HandleEnergyOptimize(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"scenario_id": "UNKNOWN",
			"error":       "Internal server processing failure.",
		})
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Malformed JSON body.",
		})
		return
	}

	scenarioID, _ := raw["scenario_id"].(string)
	if _, ok := raw["scenario_id"].(string); !ok || scenarioID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Missing mandatory parameter string: scenario_id.",
		})
		return
	}

	notesRaw, ok := raw["operator_notes"].([]any)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "operator_notes must be an array of 1 to 3 strings.",
		})
		return
	}
	if len(notesRaw) < 1 || len(notesRaw) > 3 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "operator_notes must contain 1 to 3 non-empty strings.",
		})
		return
	}
	notes := make([]string, len(notesRaw))
	for i, n := range notesRaw {
		s, ok := n.(string)
		if !ok || strings.TrimSpace(s) == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "operator_notes must contain 1 to 3 non-empty strings.",
			})
			return
		}
		notes[i] = s
	}

	hoursRaw, ok := raw["hours"].([]any)
	if !ok || len(hoursRaw) != 24 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "hours must be an array of exactly 24 hourly entries.",
		})
		return
	}
	hours := make([]optimizer.HourInput, 24)
	seenHour := map[int]bool{}
	for _, hRaw := range hoursRaw {
		hMap, ok := hRaw.(map[string]any)
		if !ok {
			badHour(w)
			return
		}
		hf, ok := hMap["hour"].(float64)
		if !ok || hf != math.Trunc(hf) || hf < 0 || hf > 23 {
			badHour(w)
			return
		}
		h := int(hf)
		if seenHour[h] {
			badHour(w)
			return
		}
		seenHour[h] = true
		demand, ok1 := numField(hMap, "demand_kwh")
		solar, ok2 := numField(hMap, "solar_kwh")
		tariff, ok3 := numField(hMap, "tariff_bdt_per_kwh")
		if !ok1 || !ok2 || !ok3 {
			badHour(w)
			return
		}
		if demand < 0 || solar < 0 || tariff < 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "hourly demand, solar, and tariff must be finite non-negative numbers.",
			})
			return
		}
		hours[h] = optimizer.HourInput{Demand: demand, Solar: solar, Tariff: tariff}
	}

	battRaw, ok := raw["battery"].(map[string]any)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "battery must include capacity_kwh, initial_energy_kwh, minimum_energy_kwh, max_charge_kwh_per_hour, max_discharge_kwh_per_hour.",
		})
		return
	}
	capacity, ok1 := numField(battRaw, "capacity_kwh")
	initial, ok2 := numField(battRaw, "initial_energy_kwh")
	minimum, ok3 := numField(battRaw, "minimum_energy_kwh")
	maxCharge, ok4 := numField(battRaw, "max_charge_kwh_per_hour")
	maxDischarge, ok5 := numField(battRaw, "max_discharge_kwh_per_hour")
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "battery must include capacity_kwh, initial_energy_kwh, minimum_energy_kwh, max_charge_kwh_per_hour, max_discharge_kwh_per_hour.",
		})
		return
	}
	batt := optimizer.BatteryConfig{
		Capacity: capacity, Initial: initial, Minimum: minimum,
		MaxCharge: maxCharge, MaxDischarge: maxDischarge,
	}
	if capacity <= 0 || minimum < 0 || initial < 0 || maxCharge < 0 || maxDischarge < 0 ||
		minimum > initial || initial > capacity {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "battery energies must satisfy 0 <= minimum_energy_kwh <= initial_energy_kwh <= capacity_kwh with non-negative rate limits.",
		})
		return
	}

	req := services.EnergyRequest{
		ScenarioID: scenarioID, OperatorNotes: notes, Battery: services.BatterySpec{
			CapacityKwh: capacity, InitialEnergyKwh: initial, MinimumEnergyKwh: minimum,
			MaxChargeKwhPerHour: maxCharge, MaxDischargeKwhPerHour: maxDischarge,
		},
	}
	for _, h := range hours {
		req.Hours = append(req.Hours, services.HourEntry{
			DemandKwh: h.Demand, SolarKwh: h.Solar, TariffBdtPerKwh: h.Tariff,
		})
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	rawInterp := services.GenerateInterpretation(ctx, req)
	sanitized := services.SanitizeInterpretation(notes, rawInterp)
	dirs := make([]optimizer.Directive, len(sanitized))
	for i, e := range sanitized {
		dirs[i] = optimizer.DecodeDirective(e.DirectiveType, e.Applies, e.StructuredAdjustment)
	}

	constraints := optimizer.BuildConstraints(hours, batt, dirs)
	plan, err := optimizer.Optimize(hours, batt, constraints)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"scenario_id": scenarioID,
			"error":       "Internal server processing failure.",
		})
		return
	}
	if err := optimizer.Replay(plan, hours, batt, constraints); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"scenario_id": scenarioID,
			"error":       "Internal server processing failure.",
		})
		return
	}

	totalGrid, totalCost, peakGrid := optimizer.Totals(plan, hours)

	interpOut := make([]any, len(sanitized))
	appliedTypes := []string{}
	for i, e := range sanitized {
		if dirs[i].Applies {
			appliedTypes = append(appliedTypes, dirs[i].Type)
		}
		interpOut[i] = map[string]any{
			"note_index":            i,
			"applies":               dirs[i].Applies && dirs[i].Type != optimizer.DNoOp,
			"directive_type":        dirs[i].Type,
			"structured_adjustment": canonicalAdjustment(dirs[i]),
			"explanation":           e.Explanation,
		}
	}
	sort.Strings(appliedTypes)
	appliedDesc := "no directives"
	if len(appliedTypes) > 0 {
		appliedDesc = strings.Join(appliedTypes, ", ")
	}

	planOut := make([]any, 24)
	for h, p := range plan {
		planOut[h] = map[string]any{
			"hour": h, "grid_kwh": p.GridKwh, "solar_used_kwh": p.SolarUsedKwh,
			"battery_action": p.BatteryAction, "battery_kwh": p.BatteryKwh,
			"battery_energy_after_kwh": p.BatteryEnergyAfterKwh,
		}
	}

	payload := map[string]any{
		"scenario_id":              scenarioID,
		"directive_interpretation": interpOut,
		"hourly_plan":              planOut,
		"total_grid_kwh":           totalGrid,
		"total_cost_bdt":           totalCost,
		"peak_grid_kwh":            peakGrid,
		"plan_summary": "Applied " + appliedDesc + " across " +
			strconv.Itoa(len(notes)) + " operator note(s); total grid " +
			strconv.FormatFloat(totalGrid, 'f', -1, 64) + " kWh costing BDT " +
			strconv.FormatFloat(totalCost, 'f', -1, 64) +
			" with peak " + strconv.FormatFloat(peakGrid, 'f', -1, 64) +
			" kWh; battery restored to initial level.",
	}
	writeJSON(w, http.StatusOK, payload)
}

func canonicalAdjustment(d optimizer.Directive) any {
	if !d.Applies || d.Type == optimizer.DNoOp {
		return nil
	}
	hours := append([]int{}, d.Hours...)
	sort.Ints(hours)
	switch d.Type {
	case optimizer.DSolarReduction:
		return map[string]any{"hours": hours, "factor": d.Factor}
	case optimizer.DMinimumBatteryReserve:
		return map[string]any{"hours": hours, "minimum_energy_kwh": d.MinimumEnergyKwh}
	case optimizer.DNoChargeWindow, optimizer.DNoDischargeWindow:
		return map[string]any{"hours": hours}
	case optimizer.DMaxGridWindow:
		return map[string]any{"hours": hours, "max_grid_kwh": d.MaxGridKwh}
	default:
		return nil
	}
}

func numField(m map[string]any, key string) (float64, bool) {
	v, ok := m[key].(float64)
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func badHour(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"error": "each hours entry must have a unique integer hour 0-23 with demand_kwh, solar_kwh, tariff_bdt_per_kwh.",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
