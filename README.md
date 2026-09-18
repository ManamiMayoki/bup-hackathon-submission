# GridWise — Smart Campus Energy Optimization (Go)

LLM-assisted energy scheduling service for the **BUP CSE Fest 2026 Hackathon,
Online Preliminary** ("GridWise" challenge). It receives a 24-hour campus
energy scenario plus 1–3 natural-language operator notes and returns both a
machine-checkable interpretation of those notes and a valid minimum-cost
24-hour operating plan.

Stdlib-only Go (`net/http`, no web framework, no solver dependencies).

## Architecture: LLM → deterministic guardrails → optimizer

```
POST /optimize-energy
  │ controller: request validation (400 / 422)
  ▼
  LLM interpretation (OpenRouter, deepseek-v4.1-flash, single call)
  │ untrusted JSON array, one entry per note
  ▼
  deterministic guardrails (count/type/hours/numbers/applies checks;
  anything invalid degrades to no_op — never invented, never a crash)
  ▼
  optimizer: exact dynamic programming over (hour × battery level)
  ▼
  independent replay validator → totals recomputed from plan → 200
```

- The LLM **only** interprets operator notes into `directive_interpretation`
  (this is the mandatory LLM path — it is never used merely for summaries).
- All math (effective solar, reserves, windows, grid caps, battery physics,
  end-of-day neutrality, totals) is deterministic code.
- If the LLM is unavailable or returns unusable output, every note safely
  degrades to `no_op` and the service still returns a valid schedule.

## Endpoints

- `GET /health` → `200 {"status":"ok"}`
- `POST /optimize-energy` → `200` interpretation + optimization plan

Validation:

- malformed JSON → `400 {"error":"Malformed JSON body."}`
- missing/non-string `scenario_id` → `400`
- `operator_notes` not an array → `400`; wrong count or empty entries → `422`
- `hours` not exactly 24 unique hours 0–23 → `400`; negative/non-finite
  numbers → `422`
- `battery` missing fields → `400`; incoherent levels
  (`minimum > initial`, `initial > capacity`, negatives) → `422`
- panic → `500 {"scenario_id": ..., "error":"Internal server processing failure."}`
  (no secrets or stack traces; the API key is sent via header, never in URLs)

## Setup

```bash
cp .env.example .env
# fill in OPENROUTER_API_KEY
```

Run locally:

```bash
go run .
```

Build:

```bash
go build -o server .
./server
```

Docker fallback:

```bash
docker build -t gridwise .
docker run -d -p 3000:3000 --env-file .env --name gridwise-container gridwise
```

Health check:

```bash
curl -s http://localhost:3000/health
# {"status":"ok"}
```

Sample request (see `instructions/` for the full 10-case pack):

```bash
curl -s http://localhost:3000/optimize-energy \
  -H 'Content-Type: application/json' \
  -d '{"scenario_id":"GRID-101",
       "operator_notes":["Solar output will drop to about 20% from 1 PM to 3 PM.",
                         "The cafeteria menu changes tomorrow."],
       "hours":[{"hour":0,"demand_kwh":180,"solar_kwh":0,"tariff_bdt_per_kwh":7}],
       "battery":{"capacity_kwh":500,"initial_energy_kwh":200,
                  "minimum_energy_kwh":50,"max_charge_kwh_per_hour":100,
                  "max_discharge_kwh_per_hour":100}}'
```

> `hours` must contain exactly 24 entries (0–23); abbreviated here.

## Public-sample test procedure

The 10 worked cases live in
`instructions/BUP_CSE_FEST_2026_Preli_Public_Sample_Cases.json`
(`case_count: 10`, each with `input`, `expected_output`, `rationale`):

```bash
# start the service, then for each case:
python3 - <<'EOF'
import json, urllib.request
pack = json.load(open('instructions/BUP_CSE_FEST_2026_Preli_Public_Sample_Cases.json'))
for c in pack['cases']:
    req = json.dumps(c['input']).encode()
    r = urllib.request.Request('http://localhost:3000/optimize-energy',
        data=req, headers={'Content-Type': 'application/json'})
    out = json.load(urllib.request.urlopen(r, timeout=60))
    exp = c['expected_output']
    got = [(e['directive_type'], e['applies']) for e in out['directive_interpretation']]
    want = [(e['directive_type'], e['applies']) for e in exp['directive_interpretation']]
    print(c['id'], 'interp_match=' , got == want,
          'cost=%.1f ref=%.1f' % (out['total_cost_bdt'], exp['total_cost_bdt']))
EOF
```

Expected result: interpretation semantics match on every case and
`total_cost_bdt` equals the reference optimal cost (equivalent optima are
accepted by the judge; exact equality is what our DP achieves locally).

## Layout

| File | Role |
|---|---|
| `main.go` | dotenv loader, env check, `ListenAndServe` on `0.0.0.0:$PORT` |
| `internal/app/app.go` | mux, `GET /health`, panic → controlled 500 |
| `internal/routes/routes.go` | `POST /optimize-energy` registration |
| `internal/controllers/energy_controller.go` | request validation, pipeline wiring, response assembly (totals recomputed, deterministic `plan_summary`) |
| `internal/services/ai_service.go` | LLM interpretation client, guardrail sanitizer, TTL cache (300s/60s/100 keys), bounded 429/503 retry, all-`no_op` safe failure |
| `internal/optimizer/optimizer.go` | directive decoding, constraint building, DP solver, totals, replay validator |

## Configuration

| Variable | Required | Purpose |
|---|---|---|
| `OPENROUTER_API_KEY` | yes, for live interpretation | OpenRouter key |
| `PORT` | no (default `3000`) | listen port |

Model/provider: OpenRouter `deepseek/deepseek-v4.1-flash`
(reasoning enabled, pinned to `deepseek`, no fallbacks) via
OpenAI-compatible `POST /api/v1/chat/completions` with
`response_format: json_object`, `temperature: 0.0`. LLM calls have
an 18s timeout inside a 20s request budget (judge limit: 30s).

## Dependencies, limitations, secret handling

- Dependencies: Go 1.22+ stdlib only. No external packages.
- Known limitations:
  - DP quantizes battery energy at 0.5 kWh (0.25 kWh fallback); scenarios
    with sub-0.25 kWh granularity in `initial_energy_kwh` may be rejected
    rather than scheduled.
  - Free-tier LLM quotas apply: on sustained 429/503 the service answers
    with valid all-`no_op` schedules (validity preserved, interpretation
    credit for those cases is forfeited). One bounded retry is attempted
    per request.
  - Infeasible scenarios (contradictory hard directives, which the
    organizers exclude from scoring) return controlled 500s.
- Secrets: keys live only in local `.env` (git-ignored, never committed,
  never logged, never returned in responses). The Docker image contains no
  baked-in credentials — pass `--env-file .env` at run time.
