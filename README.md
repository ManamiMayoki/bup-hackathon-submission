# GridWise — Smart Campus Energy Optimization (Go)

LLM-assisted energy scheduling service built for the **BUP CSE Fest 2026 Hackathon**, Online Preliminary (GridWise challenge).

Written entirely in Go using only the standard library. No web frameworks, no external solver packages.

* **Live Endpoint:** `[http://20.196.201.41]`
* **Repository:** [github.com/Twaha-Rahman/bup-hackathon-submission](https://github.com/Twaha-Rahman/bup-hackathon-submission?utm_source=gemini)

---

## Architecture Overview

```text
POST /optimize-energy
  │  controller: request validation (400 / 422)
  ▼
  LLM interpretation  — OpenRouter, single call, one entry per note
  │  output treated as untrusted JSON
  ▼
  deterministic guardrails — count, type, hours, numbers, applies semantics
  │  anything invalid degrades to no_op; never invented, never a crash
  ▼
  optimizer — exact dynamic programming over (hour × battery level)
  ▼
  independent replay validator → totals recomputed from the plan → 200

```

The language model is used for one job only: turning operator notes into `directive_interpretation`. It is never used just for summary text. Every number in the schedule — effective solar, reserves, charge and discharge windows, grid caps, battery physics, end-of-day neutrality, totals — comes from deterministic code.

If the model is unreachable or returns unusable output, all notes degrade safely to `no_op` and the service still returns a valid schedule.

---

## Prerequisites

Ensure you have the following installed on your development machine:

* **Go** (v1.22 or newer)
* **Docker** (optional, for containerized execution)

---

## Environment Configuration

Copy the example environment file and configure your API key:

```bash
cp .env.example .env

```

## Running for Development

1. Clone the repository and navigate into the directory:

```bash
git clone https://github.com/Twaha-Rahman/bup-hackathon-submission
cd bup-hackathon-submission

```

2. Run the server directly:

```bash
go run .

```

The server binds to `0.0.0.0:$PORT` (default `3000`). Verify it is up:

```bash
curl -s http://localhost:3000/health
# {"status":"ok"}

```

Alternatively, build a standalone binary:

```bash
go build -o server .
./server

```

---

## Docker Fallback

You can run the application containerized using Docker:

```bash
# Build the Docker image
docker build -t gridwise .

# Run the container
docker run -d -p 3000:3000 --env-file .env --name gridwise-container gridwise

# Verify health
curl -s http://localhost:3000/health

```

> **Note:** The image exposes port `3000`, binds to `0.0.0.0`, and contains no baked-in credentials. Pass the key at run time with `--env-file`.

---

## Configuration Reference

| Variable | Required | Purpose |
| --- | --- | --- |
| `OPENROUTER_API_KEY` | **Yes** | OpenRouter API key for live interpretation |
| `PORT` | No (default `3000`) | Server listen port |

* **Model and Provider:** OpenRouter `deepseek/deepseek-v4.1-flash`, reasoning enabled, pinned to `open-inference/fp8`, no provider fallbacks. Called over the OpenAI-compatible `POST /api/v1/chat/completions` endpoint with `response_format: json_object` and `temperature: 0.0`.
* **Caching:** Interpretations are cached (TTL 300s success, 60s failure, 100 keys).

---

## API Endpoints

### 1. Health Check

* **Method:** `GET`
* **Path:** `http://20.196.201.41/health`
* **Success Response (`200 OK`):**

```json
{ "status": "ok" }

```

### 2. Optimize Energy

* **Method:** `POST`
* **Path:** `http://20.196.201.41/optimize-energy`
* **Purpose:** Accepts one scenario object, returns the interpretation plus the 24-hour plan.

#### Sample Request

```bash
curl -s http://localhost:3000/optimize-energy \
  -H 'Content-Type: application/json' \
  -d '{
    "scenario_id": "GRID-101",
    "operator_notes": [
      "Solar output will drop to about 20% from 1 PM to 3 PM.",
      "Do not charge the battery between 2 PM and 4 PM."
    ],
    "hours": [
      {"hour": 0,  "demand_kwh": 180, "solar_kwh": 0,   "tariff_bdt_per_kwh": 7},
      {"hour": 1,  "demand_kwh": 170, "solar_kwh": 0,   "tariff_bdt_per_kwh": 6},
      {"hour": 2,  "demand_kwh": 160, "solar_kwh": 0,   "tariff_bdt_per_kwh": 6},
      {"hour": 3,  "demand_kwh": 160, "solar_kwh": 0,   "tariff_bdt_per_kwh": 6},
      {"hour": 4,  "demand_kwh": 170, "solar_kwh": 0,   "tariff_bdt_per_kwh": 7},
      {"hour": 5,  "demand_kwh": 190, "solar_kwh": 0,   "tariff_bdt_per_kwh": 8},
      {"hour": 6,  "demand_kwh": 210, "solar_kwh": 20,  "tariff_bdt_per_kwh": 10},
      {"hour": 7,  "demand_kwh": 240, "solar_kwh": 80,  "tariff_bdt_per_kwh": 12},
      {"hour": 8,  "demand_kwh": 260, "solar_kwh": 150, "tariff_bdt_per_kwh": 15},
      {"hour": 9,  "demand_kwh": 280, "solar_kwh": 220, "tariff_bdt_per_kwh": 18},
      {"hour": 10, "demand_kwh": 290, "solar_kwh": 280, "tariff_bdt_per_kwh": 20},
      {"hour": 11, "demand_kwh": 300, "solar_kwh": 320, "tariff_bdt_per_kwh": 22},
      {"hour": 12, "demand_kwh": 310, "solar_kwh": 350, "tariff_bdt_per_kwh": 25},
      {"hour": 13, "demand_kwh": 300, "solar_kwh": 340, "tariff_bdt_per_kwh": 24},
      {"hour": 14, "demand_kwh": 290, "solar_kwh": 300, "tariff_bdt_per_kwh": 22},
      {"hour": 15, "demand_kwh": 280, "solar_kwh": 220, "tariff_bdt_per_kwh": 20},
      {"hour": 16, "demand_kwh": 270, "solar_kwh": 130, "tariff_bdt_per_kwh": 18},
      {"hour": 17, "demand_kwh": 290, "solar_kwh": 40,  "tariff_bdt_per_kwh": 22},
      {"hour": 18, "demand_kwh": 320, "solar_kwh": 0,   "tariff_bdt_per_kwh": 28},
      {"hour": 19, "demand_kwh": 310, "solar_kwh": 0,   "tariff_bdt_per_kwh": 30},
      {"hour": 20, "demand_kwh": 280, "solar_kwh": 0,   "tariff_bdt_per_kwh": 26},
      {"hour": 21, "demand_kwh": 240, "solar_kwh": 0,   "tariff_bdt_per_kwh": 18},
      {"hour": 22, "demand_kwh": 210, "solar_kwh": 0,   "tariff_bdt_per_kwh": 10},
      {"hour": 23, "demand_kwh": 190, "solar_kwh": 0,   "tariff_bdt_per_kwh": 7}
    ],
    "battery": {
      "capacity_kwh": 500,
      "initial_energy_kwh": 200,
      "minimum_energy_kwh": 50,
      "max_charge_kwh_per_hour": 100,
      "max_discharge_kwh_per_hour": 100
    }
  }'

```

#### Sample Response (Abbreviated)

```json
{
  "directive_interpretation": [
    {
      "note_index": 0,
      "applies": true,
      "directive_type": "solar_reduction",
      "structured_adjustment": { "factor": 0.2, "hours": [13, 14] },
      "explanation": "Solar output drops to 20% of normal from 1 PM to 3 PM."
    },
    {
      "note_index": 1,
      "applies": true,
      "directive_type": "no_charge_window",
      "structured_adjustment": { "hours": [14, 15] },
      "explanation": "Battery charging is unavailable from 2 PM to 4 PM."
    }
  ],
  "hourly_plan": [
    {
      "hour": 0,
      "grid_kwh": 80,
      "solar_used_kwh": 0,
      "battery_action": "discharge",
      "battery_kwh": 100,
      "battery_energy_after_kwh": 100
    }
  ],
  "total_grid_kwh": 0,
  "total_cost_bdt": 0,
  "peak_grid_kwh": 0,
  "plan_summary": "..."
}

```

> **Important Conventions:**
> * A window from **1 PM to 3 PM** corresponds to hours `[13, 14]` (start included, end excluded).
> * For `solar_reduction`, the `factor` represents the remaining fraction (e.g.an 80% reduction means a factor of `0.2`).
> 
> 

---

## Validation Behavior & Error Handling

| Condition | Status Code | Response Structure / Error |
| --- | --- | --- |
| Malformed JSON body | `400` | `{"error":"Malformed JSON body."}` |
| Missing or non-string `scenario_id` | `400` | Standard error payload |
| `operator_notes` not an array | `400` | Standard error payload |
| `operator_notes` wrong count or empty entries | `422` | Standard error payload |
| Hours not exactly 24 unique hours (0–23) | `400` | Standard error payload |
| Negative or non-finite numbers | `422` | Standard error payload |
| Battery missing fields | `400` | Standard error payload |
| Incoherent battery levels (min > initial, initial > capacity, etc.) | `422` | Standard error payload |
| Internal server panic | `500` | `{"scenario_id": ..., "error":"Internal server processing failure."}` |

*No secrets or stack traces appear in any response. The API key always travels in a secure header, never in a URL.*

---

## Public-Sample Test Procedure

The 10 worked cases are located in `instructions/BUP_CSE_FEST_2026_Preli_Public_Sample_Cases.json`, each containing input, expected output, and rationale.

With the local server running, execute the verification script:

```python
python3 - <<'EOF'
import json, urllib.request
pack = json.load(open('instructions/BUP_CSE_FEST_2026_Preli_Public_Sample_Cases.json'))
for c in pack['cases']:
    req = json.dumps(c['input']).encode()
    r = urllib.request.Request('http://localhost:3000/optimize-energy',
        data=req, headers={'Content-Type': 'application/json'})
    out = json.load(urllib.request.urlopen(r, timeout=60))
    exp = c['expected_output']
    got  = [(e['directive_type'], e['applies']) for e in out['directive_interpretation']]
    want = [(e['directive_type'], e['applies']) for e in exp['directive_interpretation']]
    print(c['id'], 'interp_match=', got == want,
          'cost=%.1f ref=%.1f' % (out['total_cost_bdt'], exp['total_cost_bdt']))
EOF

```

*Expected Result:* Interpretation semantics match on every case, and `total_cost_bdt` equals the reference optimal cost. Equivalent optima are accepted by the judge; exact equality is what the local DP solver reaches.

---

## Project Layout

```text
├── main.go                       # Dotenv loader, env check, ListenAndServe on 0.0.0.0:$PORT
├── internal/
│   ├── app/
│   │   └── app.go                # Mux, GET /health, panic recovered to controlled 500
│   ├── routes/
│   │   └── routes.go             # POST /optimize-energy registration
│   ├── controllers/
│   │   └── energy_controller.go  # Request validation, pipeline wiring, response assembly
│   ├── services/
│   │   └── ai_service.go         # LLM client, guardrail sanitizer, TTL cache, bounded retry
│   └── optimizer/
│       ├── optimizer.go          # Directive decoding, constraints, DP solver, validator
│       └── optimizer_test.go     # Optimizer unit tests
└── instructions/                 # Problem statements and public sample test packs

```

Run tests locally with:

```bash
go test ./...

```

---

## Limitations & Security

* **Quantization:** The DP solver quantizes battery energy at `0.5 kWh`, with a `0.25 kWh` fallback. Scenarios with finer granularity in `initial_energy_kwh` may be rejected rather than scheduled.
* **Rate Limits:** Free-tier LLM quota applies. Under sustained `429` or `503` responses, the service returns valid all-`no_op` schedules (validity is preserved, but interpretation credit for those notes is lost). One bounded retry is attempted per request.
* **Infeasibility:** Infeasible scenarios featuring contradictory hard directives — which organizers exclude from scoring — return controlled `500` errors.
* **Secrets Management:** Keys live exclusively in a local `.env` file, which is git-ignored and never committed, logged, or returned in any response. The Docker image contains zero hardcoded credentials.