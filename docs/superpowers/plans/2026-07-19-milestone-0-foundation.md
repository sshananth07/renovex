# Milestone 0 — Project Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a runnable local development environment for the Renovation Project Intelligence Platform — Docker Compose infra, a Go+Huma backend that connects to MongoDB and serves `/health`/`/ready`, a Next.js frontend shell, foundational Money/Quantity types, platform infrastructure interfaces (storage/mail/jobs/AI), and the full set of empty domain module directories — with no business logic yet.

**Architecture:** Modular monolith per `docs/superpowers/specs/2026-07-19-phase1-foundation-design.md`. `internal/foundation` holds infra-free business primitives (Money, Quantity). `internal/platform` holds technical infrastructure (Mongo, config, logging, HTTP wiring, storage, mail, jobs, AI) behind interfaces with Local/Mock implementations. Domain modules (`identity`, `projects`, etc.) get directories and `doc.go` only — real logic starts in Milestone 1.

**Tech Stack:** Go 1.26.5 (module directive `go 1.25`), chi v5, huma/v2 + humachi adapter, mongo-driver/v2, shopspring/decimal, testcontainers-go (mongodb module), Next.js (App Router) + TypeScript, Docker Compose (MongoDB + Mailpit).

---

## Pre-flight

Go 1.26.5 and Node v24.12.0/npm 11.6.2 are already installed and confirmed working on this machine. Docker 29.4.0 / Docker Compose v5.1.1 are installed. All commands below assume PowerShell as the primary shell (per environment), with Bash-equivalent noted only where it differs meaningfully.

---

### Task 1: Root project files and directory skeleton

**Files:**
- Create: `.env.example`
- Create: `.gitignore`
- Create: `README.md`

- [ ] **Step 1: Create the directory skeleton**

Run:
```powershell
New-Item -ItemType Directory -Force -Path `
  "backend/cmd/api", `
  "backend/internal/foundation/money", `
  "backend/internal/foundation/quantity", `
  "backend/internal/platform/config", `
  "backend/internal/platform/logging", `
  "backend/internal/platform/mongo", `
  "backend/internal/platform/http", `
  "backend/internal/platform/storage", `
  "backend/internal/platform/mail", `
  "backend/internal/platform/jobs", `
  "backend/internal/platform/ai", `
  "backend/internal/identity", `
  "backend/internal/companies", `
  "backend/internal/access", `
  "backend/internal/clients", `
  "backend/internal/projects", `
  "backend/internal/properties", `
  "backend/internal/spaces", `
  "backend/internal/work", `
  "backend/internal/materials", `
  "backend/internal/suppliers", `
  "backend/internal/procurement", `
  "backend/internal/labour", `
  "backend/internal/costs", `
  "backend/internal/estimates", `
  "backend/internal/quotations", `
  "backend/internal/payments", `
  "backend/internal/documents", `
  "backend/internal/approvals", `
  "backend/internal/audit", `
  "backend/internal/ai", `
  "backend/migrations", `
  "data/uploads", `
  "docs/adr" | Out-Null
```

Expected: no errors; directories created.

- [ ] **Step 2: Create `.env.example`**

```
# --- Server ---
APP_ENV=development
HTTP_PORT=8080

# --- MongoDB ---
MONGO_URI=mongodb://localhost:27017
MONGO_DATABASE=renovation_platform

# --- JWT (reserved for Milestone 1; not used by Milestone 0) ---
JWT_ACCESS_SECRET=changeme-access-secret
JWT_REFRESH_SECRET=changeme-refresh-secret

# --- Email (Mailpit, local dev) ---
SMTP_HOST=localhost
SMTP_PORT=1025
SMTP_FROM=no-reply@renovation-platform.local

# --- File storage ---
STORAGE_LOCAL_PATH=./data/uploads

# --- AI Gateway ---
AI_PROVIDER=mock
```

- [ ] **Step 3: Create `.gitignore`**

```
# Go
backend/bin/
*.exe
*.test

# Node
apps/web/node_modules/
apps/web/.next/
apps/web/out/

# Env
.env
.env.local

# Local data
data/uploads/*
!data/uploads/.gitkeep

# OS
.DS_Store
Thumbs.db
```

- [ ] **Step 4: Create a `.gitkeep` so the uploads dir survives**

Run:
```powershell
New-Item -ItemType File -Force -Path "data/uploads/.gitkeep" | Out-Null
```

- [ ] **Step 5: Create `README.md`**

```markdown
# Renovation Project Intelligence Platform

AI-assisted renovation project costing, quotation, procurement, and profitability
platform for contractors. See [`phase1.md`](phase1.md) for full Phase 1 product
scope and [`docs/superpowers/specs/2026-07-19-phase1-foundation-design.md`](docs/superpowers/specs/2026-07-19-phase1-foundation-design.md)
for the technical architecture.

## Prerequisites

- Go 1.25+
- Node.js 20+
- Docker + Docker Compose

## Local development

1. Copy environment config:

   ```
   cp .env.example .env
   ```

2. Start infrastructure (MongoDB + Mailpit):

   ```
   docker compose up -d
   ```

   Mailpit UI: http://localhost:8025

3. Run the backend:

   ```
   cd backend
   go run ./cmd/api
   ```

   Health check: http://localhost:8080/health
   Readiness check: http://localhost:8080/ready

4. Run the frontend:

   ```
   cd apps/web
   npm install
   npm run dev
   ```

   App: http://localhost:3000

## Tests

Backend unit + MongoDB integration tests (spins up an ephemeral MongoDB
container via testcontainers-go — requires Docker running, no need to
`docker compose up` first):

```
cd backend
go test ./...
```

## Project structure

- `apps/web/` — Next.js + TypeScript frontend
- `backend/cmd/api/` — application entrypoint
- `backend/internal/foundation/` — business primitives (Money, Quantity) with no infrastructure dependencies
- `backend/internal/platform/` — technical infrastructure (Mongo, config, logging, HTTP, storage, mail, jobs, AI gateway)
- `backend/internal/<domain>/` — vertically-owned domain modules (identity, projects, quotations, etc.)
- `backend/migrations/` — MongoDB schema migration scripts
- `docs/adr/` — architectural decision records
- `docs/superpowers/specs/` — design specs
- `docs/superpowers/plans/` — implementation plans
```

- [ ] **Step 6: Verify the files are in place**

Run:
```powershell
Get-ChildItem .env.example, .gitignore, README.md, data/uploads/.gitkeep
```

Expected: all four paths listed with no errors. (This project is kept fully local — no git repository is initialized as part of this plan.)

---

### Task 2: Docker Compose infrastructure (MongoDB + Mailpit)

**Files:**
- Create: `docker-compose.yml`

- [ ] **Step 1: Write `docker-compose.yml`**

```yaml
services:
  mongo:
    image: mongo:7
    container_name: renovation-mongo
    restart: unless-stopped
    ports:
      - "27017:27017"
    volumes:
      - mongo_data:/data/db

  mailpit:
    image: axllent/mailpit:latest
    container_name: renovation-mailpit
    restart: unless-stopped
    ports:
      - "1025:1025"   # SMTP
      - "8025:8025"   # Web UI

volumes:
  mongo_data:
```

- [ ] **Step 2: Start the stack and verify**

Run:
```powershell
docker compose up -d
docker compose ps
```

Expected: both `renovation-mongo` and `renovation-mailpit` show `running`/`Up`.

- [ ] **Step 3: Verify Mailpit UI is reachable**

Run:
```powershell
Invoke-WebRequest -Uri "http://localhost:8025" -UseBasicParsing | Select-Object -ExpandProperty StatusCode
```

Expected: `200`.

---

### Task 3: Go module init and dependencies

**Files:**
- Create: `backend/go.mod`
- Create: `backend/go.sum` (generated)

- [ ] **Step 1: Initialize the Go module**

Run (from `backend/`):
```powershell
cd backend
go mod init github.com/shananth/renovation-platform/backend
```

Expected: `go.mod` created with `module github.com/shananth/renovation-platform/backend`.

- [ ] **Step 2: Set the Go directive to satisfy Huma v2's minimum**

Huma v2 requires Go 1.25+. Edit `backend/go.mod` so the second line reads:

```
go 1.25
```

- [ ] **Step 3: Add core dependencies**

Run:
```powershell
go get github.com/go-chi/chi/v5@latest
go get github.com/danielgtaylor/huma/v2@latest
go get go.mongodb.org/mongo-driver/v2@latest
go get github.com/shopspring/decimal@latest
go get github.com/joho/godotenv@latest
go get github.com/rs/zerolog@latest
```

Expected: each command exits 0 and adds a `require` line to `go.mod`.

- [ ] **Step 4: Add test-only dependencies**

Run:
```powershell
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/testcontainers/testcontainers-go/modules/mongodb@latest
go get github.com/stretchr/testify@latest
```

- [ ] **Step 5: Tidy**

Run:
```powershell
go mod tidy
```

Expected: `go.sum` created/updated, no errors.

---

### Task 4: `foundation/money` — Money, RateBPS, and rounding policy

**Files:**
- Create: `backend/internal/foundation/money/money.go`
- Create: `backend/internal/foundation/money/rate.go`
- Create: `backend/internal/foundation/money/rounding.go`
- Test: `backend/internal/foundation/money/money_test.go`
- Test: `backend/internal/foundation/money/rounding_test.go`

- [ ] **Step 1: Write the failing test for `Money` arithmetic and currency validation**

`backend/internal/foundation/money/money_test.go`:
```go
package money

import "testing"

func TestNewMoneyFromMinorUnits(t *testing.T) {
	m := New(1850, "MYR")
	if m.Amount != 1850 {
		t.Fatalf("expected amount 1850, got %d", m.Amount)
	}
	if m.Currency != "MYR" {
		t.Fatalf("expected currency MYR, got %s", m.Currency)
	}
}

func TestMoneyAddSameCurrency(t *testing.T) {
	a := New(1000, "MYR")
	b := New(500, "MYR")

	result, err := a.Add(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount != 1500 {
		t.Fatalf("expected 1500, got %d", result.Amount)
	}
}

func TestMoneyAddDifferentCurrencyReturnsError(t *testing.T) {
	a := New(1000, "MYR")
	b := New(500, "SGD")

	_, err := a.Add(b)
	if err == nil {
		t.Fatal("expected error when adding different currencies, got nil")
	}
}

func TestMoneySubtractSameCurrency(t *testing.T) {
	a := New(1500, "MYR")
	b := New(500, "MYR")

	result, err := a.Subtract(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Amount != 1000 {
		t.Fatalf("expected 1000, got %d", result.Amount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/foundation/money/... -run TestNewMoneyFromMinorUnits -v
```

Expected: FAIL — `package money` has no `New`/`Money` symbols (compile error), confirming nothing exists yet.

- [ ] **Step 3: Implement `Money`**

`backend/internal/foundation/money/money.go`:
```go
// Package money provides the canonical authoritative representation of
// financial amounts in the platform: int64 minor units plus an ISO 4217-style
// currency code. Never use float32/float64 for authoritative money values.
package money

import "fmt"

// Money is an amount expressed in the smallest unit of its currency
// (e.g. cents for MYR/SGD/USD), never a floating-point major-unit value.
type Money struct {
	Amount   int64  `bson:"amount" json:"amount"`
	Currency string `bson:"currency" json:"currency"`
}

// New constructs a Money value from a minor-unit integer amount and an
// ISO 4217-style currency code (e.g. "MYR").
func New(amountMinorUnits int64, currency string) Money {
	return Money{Amount: amountMinorUnits, Currency: currency}
}

// Add returns a+b. It returns an error if the currencies differ.
func (a Money) Add(b Money) (Money, error) {
	if a.Currency != b.Currency {
		return Money{}, fmt.Errorf("money: cannot add %s to %s", b.Currency, a.Currency)
	}
	return Money{Amount: a.Amount + b.Amount, Currency: a.Currency}, nil
}

// Subtract returns a-b. It returns an error if the currencies differ.
func (a Money) Subtract(b Money) (Money, error) {
	if a.Currency != b.Currency {
		return Money{}, fmt.Errorf("money: cannot subtract %s from %s", b.Currency, a.Currency)
	}
	return Money{Amount: a.Amount - b.Amount, Currency: a.Currency}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```powershell
go test ./internal/foundation/money/... -v
```

Expected: PASS (all four tests).

- [ ] **Step 5: Write the failing test for `RateBPS`**

Append to `backend/internal/foundation/money/money_test.go`:
```go
func TestRateBPSConstants(t *testing.T) {
	if RateBPS(600) != 600 {
		t.Fatal("RateBPS should be a plain int64-based type")
	}
}
```

- [ ] **Step 6: Implement `RateBPS`**

`backend/internal/foundation/money/rate.go`:
```go
package money

// RateBPS represents a fixed-point percentage rate expressed in basis
// points (1% = 100 bps). Authoritative rate/percentage calculations
// (tax, markup, margin) must use RateBPS, never a floating-point percentage.
type RateBPS int64

// BasisPointsDenominator is the divisor representing 100% in basis points.
const BasisPointsDenominator = 10000
```

- [ ] **Step 7: Run tests to verify they pass**

Run:
```powershell
go test ./internal/foundation/money/... -v
```

Expected: PASS.

- [ ] **Step 8: Write the failing test for the centralized rounding policy**

`backend/internal/foundation/money/rounding_test.go`:
```go
package money

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestRoundToMinorUnitsHalfUp(t *testing.T) {
	cases := []struct {
		name     string
		input    string // decimal string, in major currency units
		expected int64  // expected minor units after rounding
	}{
		{"exact value", "966.12", 96612},
		{"round up at .5 of a cent", "966.125", 96613},
		{"round down below .5 of a cent", "966.124", 96612},
		{"round up below .5 of a cent on the other side", "966.126", 96613},
		{"zero", "0", 0},
		{"negative exact", "-10.00", -1000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := decimal.NewFromString(tc.input)
			if err != nil {
				t.Fatalf("failed to parse decimal %q: %v", tc.input, err)
			}

			got := RoundToMinorUnits(d)
			if got != tc.expected {
				t.Fatalf("RoundToMinorUnits(%s) = %d, want %d", tc.input, got, tc.expected)
			}
		})
	}
}
```

- [ ] **Step 9: Run test to verify it fails**

Run:
```powershell
go test ./internal/foundation/money/... -run TestRoundToMinorUnitsHalfUp -v
```

Expected: FAIL — `RoundToMinorUnits` undefined.

- [ ] **Step 10: Implement the rounding policy**

`backend/internal/foundation/money/rounding.go`:
```go
package money

import "github.com/shopspring/decimal"

// RoundToMinorUnits applies the platform's single, centralized rounding
// policy for converting a precise decimal major-currency-unit amount into
// authoritative int64 minor units: round-half-up to 2 decimal places, then
// scale to minor units.
//
// This is the only place authoritative monetary rounding may happen.
// Domain modules must call this function rather than reimplementing
// rounding logic.
func RoundToMinorUnits(amountMajorUnits decimal.Decimal) int64 {
	rounded := amountMajorUnits.RoundBank(2) // placeholder overwritten below
	_ = rounded
	return roundHalfUpToMinorUnits(amountMajorUnits)
}

func roundHalfUpToMinorUnits(amountMajorUnits decimal.Decimal) int64 {
	scaled := amountMajorUnits.Shift(2) // move 2 decimal places -> minor units
	half := decimal.NewFromFloat(0.5)

	if scaled.Sign() >= 0 {
		return scaled.Add(half).Floor().IntPart()
	}
	// For negatives, half-up means rounding toward positive infinity at the
	// midpoint is NOT what we want; round-half-up on a negative value rounds
	// away from zero at the .5 boundary is avoided — we round half-up in the
	// conventional sense (toward +infinity) is inconsistent for negatives,
	// so mirror the positive-side rule: round the absolute value half-up,
	// then reapply the sign.
	absScaled := scaled.Neg()
	roundedAbs := absScaled.Add(half).Floor().IntPart()
	return -roundedAbs
}
```

- [ ] **Step 11: Run test to verify it passes**

Run:
```powershell
go test ./internal/foundation/money/... -v
```

Expected: PASS for all cases, including `-10.00 -> -1000`.

- [ ] **Step 12: Remove the unused placeholder line**

Edit `backend/internal/foundation/money/rounding.go`, replace the whole `RoundToMinorUnits` function with:
```go
// RoundToMinorUnits applies the platform's single, centralized rounding
// policy for converting a precise decimal major-currency-unit amount into
// authoritative int64 minor units: round-half-up (ties away from zero) to
// the nearest minor unit.
//
// This is the only place authoritative monetary rounding may happen.
// Domain modules must call this function rather than reimplementing
// rounding logic.
func RoundToMinorUnits(amountMajorUnits decimal.Decimal) int64 {
	return roundHalfUpToMinorUnits(amountMajorUnits)
}
```

- [ ] **Step 13: Run full package test suite to verify it still passes**

Run:
```powershell
go test ./internal/foundation/money/... -v
```

Expected: PASS.

---

### Task 5: `foundation/quantity` — Quantity type

**Files:**
- Create: `backend/internal/foundation/quantity/quantity.go`
- Test: `backend/internal/foundation/quantity/quantity_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/foundation/quantity/quantity_test.go`:
```go
package quantity

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestNewQuantity(t *testing.T) {
	q, err := New("32.75", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Unit != "m2" {
		t.Fatalf("expected unit m2, got %s", q.Unit)
	}
	expected := decimal.RequireFromString("32.75")
	if !q.Value.Equal(expected) {
		t.Fatalf("expected value 32.75, got %s", q.Value.String())
	}
}

func TestNewQuantityInvalidValue(t *testing.T) {
	_, err := New("not-a-number", "m2")
	if err == nil {
		t.Fatal("expected error for invalid decimal string, got nil")
	}
}

func TestQuantityMultiplyByUnitPrice(t *testing.T) {
	q, err := New("30", "m2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unitPrice := decimal.RequireFromString("29.50")

	total := q.Value.Mul(unitPrice)
	expected := decimal.RequireFromString("885.00")
	if !total.Equal(expected) {
		t.Fatalf("expected 885.00, got %s", total.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/foundation/quantity/... -v
```

Expected: FAIL — `quantity.New` undefined.

- [ ] **Step 3: Implement `Quantity`**

`backend/internal/foundation/quantity/quantity.go`:
```go
// Package quantity provides the authoritative representation of physical
// construction quantities (area, length, volume, count) as precise decimal
// values. Never use float32/float64 for authoritative quantity calculations.
package quantity

import "github.com/shopspring/decimal"

// Quantity is a precise decimal amount paired with a unit of measure
// (e.g. "m2", "kg", "bag", "unit").
type Quantity struct {
	Value decimal.Decimal
	Unit  string
}

// New constructs a Quantity from a decimal string and a unit.
// Returns an error if value is not a valid decimal string.
func New(value string, unit string) (Quantity, error) {
	d, err := decimal.NewFromString(value)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{Value: d, Unit: unit}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```powershell
go test ./internal/foundation/quantity/... -v
```

Expected: PASS.

---

### Task 6: `platform/config` — environment configuration loader

**Files:**
- Create: `backend/internal/platform/config/config.go`
- Test: `backend/internal/platform/config/config_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/platform/config/config_test.go`:
```go
package config

import "testing"

func TestLoadFromEnvUsesDefaults(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")
	t.Setenv("AI_PROVIDER", "mock")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HTTPPort != "8080" {
		t.Fatalf("expected default HTTPPort 8080, got %s", cfg.HTTPPort)
	}
	if cfg.MongoURI != "mongodb://localhost:27017" {
		t.Fatalf("expected MongoURI from env, got %s", cfg.MongoURI)
	}
	if cfg.MongoDatabase != "renovation_platform_test" {
		t.Fatalf("expected MongoDatabase from env, got %s", cfg.MongoDatabase)
	}
	if cfg.AIProvider != "mock" {
		t.Fatalf("expected AIProvider mock, got %s", cfg.AIProvider)
	}
}

func TestLoadFromEnvMissingMongoURIFails(t *testing.T) {
	t.Setenv("MONGO_URI", "")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when MONGO_URI is unset, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/platform/config/... -v
```

Expected: FAIL — `config.LoadFromEnv` undefined.

- [ ] **Step 3: Implement the config loader**

`backend/internal/platform/config/config.go`:
```go
// Package config loads typed application configuration from environment
// variables (optionally backed by a local .env file for development).
package config

import (
	"fmt"
	"os"
)

// Config holds all application configuration. Fields are populated from
// environment variables; see .env.example at the repo root for the full
// set of supported variables and their defaults.
type Config struct {
	AppEnv   string
	HTTPPort string

	MongoURI      string
	MongoDatabase string

	// JWTAccessSecret and JWTRefreshSecret are reserved for Milestone 1
	// authentication and are not used by Milestone 0.
	JWTAccessSecret  string
	JWTRefreshSecret string

	SMTPHost string
	SMTPPort string
	SMTPFrom string

	StorageLocalPath string

	AIProvider string
}

// LoadFromEnv reads configuration from process environment variables,
// applying defaults where documented in .env.example. It returns an error
// if a required variable is missing.
func LoadFromEnv() (Config, error) {
	cfg := Config{
		AppEnv:   getEnvOrDefault("APP_ENV", "development"),
		HTTPPort: getEnvOrDefault("HTTP_PORT", "8080"),

		MongoURI:      os.Getenv("MONGO_URI"),
		MongoDatabase: os.Getenv("MONGO_DATABASE"),

		JWTAccessSecret:  os.Getenv("JWT_ACCESS_SECRET"),
		JWTRefreshSecret: os.Getenv("JWT_REFRESH_SECRET"),

		SMTPHost: getEnvOrDefault("SMTP_HOST", "localhost"),
		SMTPPort: getEnvOrDefault("SMTP_PORT", "1025"),
		SMTPFrom: getEnvOrDefault("SMTP_FROM", "no-reply@renovation-platform.local"),

		StorageLocalPath: getEnvOrDefault("STORAGE_LOCAL_PATH", "./data/uploads"),

		AIProvider: getEnvOrDefault("AI_PROVIDER", "mock"),
	}

	if cfg.MongoURI == "" {
		return Config{}, fmt.Errorf("config: MONGO_URI is required")
	}
	if cfg.MongoDatabase == "" {
		return Config{}, fmt.Errorf("config: MONGO_DATABASE is required")
	}

	return cfg, nil
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```powershell
go test ./internal/platform/config/... -v
```

Expected: PASS.

---

### Task 7: `platform/logging` — structured logger

**Files:**
- Create: `backend/internal/platform/logging/logging.go`
- Test: `backend/internal/platform/logging/logging_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/platform/logging/logging_test.go`:
```go
package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewWritesJSONWithLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, "info")

	logger.Info().Str("component", "test").Msg("hello world")

	output := buf.String()
	if !strings.Contains(output, `"message":"hello world"`) {
		t.Fatalf("expected message field in output, got: %s", output)
	}
	if !strings.Contains(output, `"component":"test"`) {
		t.Fatalf("expected component field in output, got: %s", output)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/platform/logging/... -v
```

Expected: FAIL — `logging.New` undefined.

- [ ] **Step 3: Implement the logger**

`backend/internal/platform/logging/logging.go`:
```go
// Package logging provides structured JSON logging for the application,
// built on zerolog.
package logging

import (
	"io"

	"github.com/rs/zerolog"
)

// New constructs a structured JSON logger writing to w, filtered to the
// given minimum level ("debug", "info", "warn", "error"). An unrecognized
// level falls back to "info".
func New(w io.Writer, level string) zerolog.Logger {
	parsedLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		parsedLevel = zerolog.InfoLevel
	}

	return zerolog.New(w).
		Level(parsedLevel).
		With().
		Timestamp().
		Logger()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```powershell
go test ./internal/platform/logging/... -v
```

Expected: PASS.

---

### Task 8: `platform/mongo` — client factory and ping helper

**Files:**
- Create: `backend/internal/platform/mongo/mongo.go`
- Test: `backend/internal/platform/mongo/mongo_test.go`

- [ ] **Step 1: Write the failing integration test using testcontainers-go**

`backend/internal/platform/mongo/mongo_test.go`:
```go
package mongo_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func TestConnectAndPing(t *testing.T) {
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() {
		_ = platformmongo.Disconnect(context.Background(), client)
	}()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := platformmongo.Ping(pingCtx, client); err != nil {
		t.Fatalf("expected ping to succeed, got error: %v", err)
	}
}

func TestPingFailsWhenUnreachable(t *testing.T) {
	// Port 1 is reserved and nothing will be listening there.
	client, err := platformmongo.Connect("mongodb://localhost:1/?connectTimeoutMS=500&serverSelectionTimeoutMS=500")
	if err != nil {
		t.Fatalf("Connect itself should not fail (lazy connection): %v", err)
	}
	defer func() {
		_ = platformmongo.Disconnect(context.Background(), client)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := platformmongo.Ping(ctx, client); err == nil {
		t.Fatal("expected ping to an unreachable host to fail, got nil error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/platform/mongo/... -v
```

Expected: FAIL — compile error, `mongo.Connect`/`mongo.Ping`/`mongo.Disconnect` undefined in the platform package.

- [ ] **Step 3: Implement the Mongo client factory**

`backend/internal/platform/mongo/mongo.go`:
```go
// Package mongo centralizes MongoDB client creation and lifecycle
// management for the application. Domain modules receive a *mongo.Database
// from here and own their own collections; this package does not expose a
// generic cross-domain repository.
package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Connect builds a MongoDB client for the given connection URI. Connection
// is lazy: this call does not itself verify connectivity — call Ping to do
// that (e.g. from a /ready handler).
func Connect(uri string) (*mongo.Client, error) {
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	clientOpts := options.Client().
		ApplyURI(uri).
		SetServerAPIOptions(serverAPI)

	return mongo.Connect(clientOpts)
}

// Ping verifies the client can reach the primary within ctx's deadline.
func Ping(ctx context.Context, client *mongo.Client) error {
	return client.Ping(ctx, readpref.Primary())
}

// Disconnect closes the client's connections.
func Disconnect(ctx context.Context, client *mongo.Client) error {
	return client.Disconnect(ctx)
}

// Database returns a handle to the named database on the given client.
func Database(client *mongo.Client, name string) *mongo.Database {
	return client.Database(name)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```powershell
go test ./internal/platform/mongo/... -v
```

Expected: PASS (both tests). Note: the first run will download the `mongo:7` image via Docker and may take 30-60s; subsequent runs are fast.

---

### Task 9: `platform/storage` — FileStorage interface + Local implementation

**Files:**
- Create: `backend/internal/platform/storage/storage.go`
- Create: `backend/internal/platform/storage/local.go`
- Test: `backend/internal/platform/storage/local_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/platform/storage/local_test.go`:
```go
package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/storage"
)

func TestLocalFileStorageSaveAndOpen(t *testing.T) {
	dir := t.TempDir()
	fs := storage.NewLocalFileStorage(dir)

	ctx := context.Background()
	content := []byte("hello quotation pdf")

	key, err := fs.Save(ctx, "quotations/QT-0001.pdf", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("unexpected error saving: %v", err)
	}
	if key != "quotations/QT-0001.pdf" {
		t.Fatalf("expected key to be echoed back, got %s", key)
	}

	// File should actually exist on disk under dir.
	if _, err := os.Stat(filepath.Join(dir, "quotations", "QT-0001.pdf")); err != nil {
		t.Fatalf("expected file to exist on disk: %v", err)
	}

	reader, err := fs.Open(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error opening: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error reading: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("expected content %q, got %q", content, got)
	}
}

func TestLocalFileStorageOpenMissingKeyFails(t *testing.T) {
	dir := t.TempDir()
	fs := storage.NewLocalFileStorage(dir)

	_, err := fs.Open(context.Background(), "does/not/exist.pdf")
	if err == nil {
		t.Fatal("expected error opening missing key, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/platform/storage/... -v
```

Expected: FAIL — `storage.NewLocalFileStorage` undefined.

- [ ] **Step 3: Define the `FileStorage` interface**

`backend/internal/platform/storage/storage.go`:
```go
// Package storage defines the FileStorage abstraction used for documents
// (Quotation PDFs, RFQ PDFs, project photos, etc.). Phase 1 uses a local
// filesystem implementation; a future S3-backed implementation can satisfy
// the same interface without changes to calling domain modules.
package storage

import (
	"context"
	"io"
)

// FileStorage saves and retrieves opaque file content addressed by a
// caller-chosen key (e.g. "quotations/QT-0001.pdf").
type FileStorage interface {
	// Save writes content under key, creating any intermediate structure
	// as needed, and returns the key it was stored under.
	Save(ctx context.Context, key string, content io.Reader) (string, error)

	// Open returns a reader for the content stored under key. Callers must
	// close the returned reader. Returns an error if key does not exist.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}
```

- [ ] **Step 4: Implement `LocalFileStorage`**

`backend/internal/platform/storage/local.go`:
```go
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalFileStorage implements FileStorage on the local filesystem, rooted
// at a base directory (e.g. ./data/uploads).
type LocalFileStorage struct {
	baseDir string
}

// NewLocalFileStorage constructs a LocalFileStorage rooted at baseDir.
func NewLocalFileStorage(baseDir string) *LocalFileStorage {
	return &LocalFileStorage{baseDir: baseDir}
}

func (s *LocalFileStorage) Save(_ context.Context, key string, content io.Reader) (string, error) {
	fullPath := filepath.Join(s.baseDir, filepath.FromSlash(key))

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("storage: failed to create directory: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("storage: failed to create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, content); err != nil {
		return "", fmt.Errorf("storage: failed to write file: %w", err)
	}

	return key, nil
}

func (s *LocalFileStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(s.baseDir, filepath.FromSlash(key))

	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to open file %q: %w", key, err)
	}
	return f, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run:
```powershell
go test ./internal/platform/storage/... -v
```

Expected: PASS.

---

### Task 10: `platform/mail` — EmailSender interface + SMTP implementation

**Files:**
- Create: `backend/internal/platform/mail/mail.go`
- Create: `backend/internal/platform/mail/smtp.go`
- Test: `backend/internal/platform/mail/smtp_test.go`

- [ ] **Step 1: Define the `EmailSender` interface**

`backend/internal/platform/mail/mail.go`:
```go
// Package mail defines the EmailSender abstraction used to deliver
// transactional email (Client Quotation notifications, Supplier RFQ
// Invitations). Phase 1 uses a local SMTP implementation pointed at
// Mailpit for development; a future managed email provider can satisfy
// the same interface without changes to calling domain modules.
package mail

import "context"

// Message is a plain-text/HTML email to send.
type Message struct {
	To      string
	Subject string
	Body    string
}

// EmailSender sends a Message.
type EmailSender interface {
	Send(ctx context.Context, msg Message) error
}
```

- [ ] **Step 2: Write the failing test**

`backend/internal/platform/mail/smtp_test.go`:
```go
package mail_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

func TestSMTPSenderConstructsWithoutError(t *testing.T) {
	sender := mail.NewSMTPSender("localhost", "1025", "no-reply@renovation-platform.local")
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
}

// TestSMTPSenderSendRequiresRecipient verifies input validation happens
// before any network call is attempted, so this test has no dependency on
// Mailpit actually running.
func TestSMTPSenderSendRequiresRecipient(t *testing.T) {
	sender := mail.NewSMTPSender("localhost", "1025", "no-reply@renovation-platform.local")

	err := sender.Send(context.Background(), mail.Message{
		To:      "",
		Subject: "Test",
		Body:    "Body",
	})
	if err == nil {
		t.Fatal("expected error for empty recipient, got nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/platform/mail/... -v
```

Expected: FAIL — `mail.NewSMTPSender` undefined.

- [ ] **Step 4: Implement the SMTP sender**

`backend/internal/platform/mail/smtp.go`:
```go
package mail

import (
	"context"
	"fmt"
	"net/smtp"
)

// SMTPSender sends email via a plain SMTP relay (e.g. Mailpit for local
// development; a real relay in production).
type SMTPSender struct {
	host string
	port string
	from string
}

// NewSMTPSender constructs an SMTPSender targeting host:port, sending as from.
func NewSMTPSender(host, port, from string) *SMTPSender {
	return &SMTPSender{host: host, port: port, from: from}
}

func (s *SMTPSender) Send(_ context.Context, msg Message) error {
	if msg.To == "" {
		return fmt.Errorf("mail: recipient (To) is required")
	}

	addr := fmt.Sprintf("%s:%s", s.host, s.port)
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		s.from, msg.To, msg.Subject, msg.Body)

	// Mailpit accepts unauthenticated SMTP, so no auth is configured here.
	return smtp.SendMail(addr, nil, s.from, []string{msg.To}, []byte(body))
}
```

- [ ] **Step 5: Run test to verify it passes**

Run:
```powershell
go test ./internal/platform/mail/... -v
```

Expected: PASS (both tests; the recipient-validation test does not require Mailpit to be running since it fails before any network call).

---

### Task 11: `platform/jobs` — JobQueue interface + in-process implementation

**Files:**
- Create: `backend/internal/platform/jobs/jobs.go`
- Create: `backend/internal/platform/jobs/inprocess.go`
- Test: `backend/internal/platform/jobs/inprocess_test.go`

- [ ] **Step 1: Define the `JobQueue` interface**

`backend/internal/platform/jobs/jobs.go`:
```go
// Package jobs defines the JobQueue abstraction used for asynchronous
// background work (e.g. PDF generation, AI processing requests). Phase 1
// uses a non-durable in-process implementation; a future SQS-backed queue
// can satisfy the same interface without changes to calling domain modules.
package jobs

import "context"

// Job is a unit of asynchronous work.
type Job struct {
	Name    string
	Payload map[string]any
}

// Handler processes a Job.
type Handler func(ctx context.Context, job Job) error

// JobQueue enqueues Jobs for asynchronous processing.
type JobQueue interface {
	// Enqueue submits job for processing. Enqueue does not guarantee
	// delivery across process restarts (non-durable in the Phase 1
	// in-process implementation).
	Enqueue(ctx context.Context, job Job) error

	// RegisterHandler associates a Handler with jobs of the given name.
	RegisterHandler(name string, handler Handler)
}
```

- [ ] **Step 2: Write the failing test**

`backend/internal/platform/jobs/inprocess_test.go`:
```go
package jobs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/jobs"
)

func TestInProcessQueueDispatchesToRegisteredHandler(t *testing.T) {
	q := jobs.NewInProcessQueue()
	defer q.Close()

	var mu sync.Mutex
	var received jobs.Job
	done := make(chan struct{})

	q.RegisterHandler("send_quotation_email", func(_ context.Context, job jobs.Job) error {
		mu.Lock()
		received = job
		mu.Unlock()
		close(done)
		return nil
	})

	err := q.Enqueue(context.Background(), jobs.Job{
		Name:    "send_quotation_email",
		Payload: map[string]any{"quotationId": "QT-0001"},
	})
	if err != nil {
		t.Fatalf("unexpected error enqueueing: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to run")
	}

	mu.Lock()
	defer mu.Unlock()
	if received.Payload["quotationId"] != "QT-0001" {
		t.Fatalf("expected payload to be delivered to handler, got %+v", received)
	}
}

func TestInProcessQueueEnqueueWithoutHandlerFails(t *testing.T) {
	q := jobs.NewInProcessQueue()
	defer q.Close()

	err := q.Enqueue(context.Background(), jobs.Job{Name: "unregistered_job"})
	if err == nil {
		t.Fatal("expected error enqueueing a job with no registered handler, got nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/platform/jobs/... -v
```

Expected: FAIL — `jobs.NewInProcessQueue` undefined.

- [ ] **Step 4: Implement the in-process queue**

`backend/internal/platform/jobs/inprocess.go`:
```go
package jobs

import (
	"context"
	"fmt"
	"sync"
)

// InProcessQueue is a non-durable JobQueue that dispatches jobs to
// registered handlers on goroutines within the current process. Jobs are
// lost on process restart — acceptable for Phase 1 local development;
// a durable, distributed queue is out of scope.
type InProcessQueue struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	wg       sync.WaitGroup
}

// NewInProcessQueue constructs a ready-to-use InProcessQueue.
func NewInProcessQueue() *InProcessQueue {
	return &InProcessQueue{
		handlers: make(map[string]Handler),
	}
}

func (q *InProcessQueue) RegisterHandler(name string, handler Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[name] = handler
}

func (q *InProcessQueue) Enqueue(ctx context.Context, job Job) error {
	q.mu.RLock()
	handler, ok := q.handlers[job.Name]
	q.mu.RUnlock()

	if !ok {
		return fmt.Errorf("jobs: no handler registered for job %q", job.Name)
	}

	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		_ = handler(ctx, job)
	}()

	return nil
}

// Close blocks until all in-flight jobs have completed.
func (q *InProcessQueue) Close() {
	q.wg.Wait()
}
```

- [ ] **Step 5: Run test to verify it passes**

Run:
```powershell
go test ./internal/platform/jobs/... -v
```

Expected: PASS.

---

### Task 12: `platform/ai` — AIGateway interface + Mock provider

**Files:**
- Create: `backend/internal/platform/ai/ai.go`
- Create: `backend/internal/platform/ai/mock.go`
- Test: `backend/internal/platform/ai/mock_test.go`

- [ ] **Step 1: Define the `AIGateway` interface**

`backend/internal/platform/ai/ai.go`:
```go
// Package ai defines the AIGateway abstraction the rest of the application
// calls into for AI-assisted suggestions (work item suggestions, resource
// suggestions, quotation wording, etc.). Phase 1 defaults to a mock
// provider (AI_PROVIDER=mock) so the application can run and be tested
// without any external AI API. A real provider can satisfy the same
// interface later without changes to calling domain modules.
//
// Per phase1.md's AI principle, callers must never treat AIGateway output
// as authoritative: it always produces suggestions for a human to review.
package ai

import "context"

// SuggestionRequest is a generic request for an AI-generated suggestion.
// Domain modules build a SuggestionRequest from their own structured,
// authorized context — the AIGateway never queries the database directly.
type SuggestionRequest struct {
	Kind    string // e.g. "work_item_suggestion", "quotation_wording"
	Context map[string]any
}

// SuggestionResponse is a generic AI-generated suggestion, always subject
// to contractor review before becoming authoritative domain data.
type SuggestionResponse struct {
	Kind    string
	Content string
}

// AIGateway produces suggestions from structured, pre-authorized context.
type AIGateway interface {
	Suggest(ctx context.Context, req SuggestionRequest) (SuggestionResponse, error)
}
```

- [ ] **Step 2: Write the failing test**

`backend/internal/platform/ai/mock_test.go`:
```go
package ai_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/ai"
)

func TestMockProviderReturnsDeterministicSuggestion(t *testing.T) {
	provider := ai.NewMockProvider()

	resp, err := provider.Suggest(context.Background(), ai.SuggestionRequest{
		Kind: "work_item_suggestion",
		Context: map[string]any{
			"description": "Renovate master bathroom",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Kind != "work_item_suggestion" {
		t.Fatalf("expected response Kind to echo request Kind, got %s", resp.Kind)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty mock suggestion content")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/platform/ai/... -v
```

Expected: FAIL — `ai.NewMockProvider` undefined.

- [ ] **Step 4: Implement the mock provider**

`backend/internal/platform/ai/mock.go`:
```go
package ai

import (
	"context"
	"fmt"
)

// MockProvider is a deterministic, offline AIGateway implementation used
// as the Phase 1 default (AI_PROVIDER=mock) so the application runs and is
// fully testable without any external AI API.
type MockProvider struct{}

// NewMockProvider constructs a MockProvider.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) Suggest(_ context.Context, req SuggestionRequest) (SuggestionResponse, error) {
	return SuggestionResponse{
		Kind:    req.Kind,
		Content: fmt.Sprintf("[mock suggestion for %s]", req.Kind),
	}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run:
```powershell
go test ./internal/platform/ai/... -v
```

Expected: PASS.

---

### Task 13: Domain module placeholders (doc.go only)

**Files:**
- Create: `backend/internal/identity/doc.go`
- Create: `backend/internal/companies/doc.go`
- Create: `backend/internal/access/doc.go`
- Create: `backend/internal/clients/doc.go`
- Create: `backend/internal/projects/doc.go`
- Create: `backend/internal/properties/doc.go`
- Create: `backend/internal/spaces/doc.go`
- Create: `backend/internal/work/doc.go`
- Create: `backend/internal/materials/doc.go`
- Create: `backend/internal/suppliers/doc.go`
- Create: `backend/internal/procurement/doc.go`
- Create: `backend/internal/labour/doc.go`
- Create: `backend/internal/costs/doc.go`
- Create: `backend/internal/estimates/doc.go`
- Create: `backend/internal/quotations/doc.go`
- Create: `backend/internal/payments/doc.go`
- Create: `backend/internal/documents/doc.go`
- Create: `backend/internal/approvals/doc.go`
- Create: `backend/internal/audit/doc.go`
- Create: `backend/internal/ai/doc.go`

- [ ] **Step 1: Create each `doc.go`**

Each file follows the same pattern — a package doc comment naming the module's future responsibility per `phase1.md`, and nothing else. No handler/service/repository code yet (per Milestone 0 scope).

`backend/internal/identity/doc.go`:
```go
// Package identity owns User accounts, password credentials, and
// authentication (login, JWT access/refresh tokens). See phase1.md §2.
// Implementation begins in Milestone 1.
package identity
```

`backend/internal/companies/doc.go`:
```go
// Package companies owns Company (tenant) records and Company Membership
// (User-to-Company role assignment: Owner, Admin, Employee). See
// phase1.md §2-3. Implementation begins in Milestone 1.
package companies
```

`backend/internal/access/doc.go`:
```go
// Package access owns Access Grants: scoped, token-based external access
// for Clients (Quotation review) and Suppliers (RFQ response), independent
// of Company Membership. See phase1.md §2, §57. Implementation begins in
// Milestone 5 (Client access) and Milestone 6 (Supplier access).
package access
```

`backend/internal/clients/doc.go`:
```go
// Package clients owns Client (customer) records, separate from Projects
// so a returning Client can have multiple renovation jobs. See phase1.md
// §4. Implementation begins in Milestone 2.
package clients
```

`backend/internal/projects/doc.go`:
```go
// Package projects owns the Project entity: the root business object
// referencing Client, Property, Spaces, and (by reference) all downstream
// financial and procurement records. See phase1.md §5. Implementation
// begins in Milestone 2.
package projects
```

`backend/internal/properties/doc.go`:
```go
// Package properties owns Property records: the physical location a
// Project renovates. See phase1.md §6. Implementation begins in
// Milestone 2.
package properties
```

`backend/internal/spaces/doc.go`:
```go
// Package spaces owns Space records: physical areas (rooms) within a
// Project's Property, used to organize Work Items. See phase1.md §7.
// Implementation begins in Milestone 2.
package spaces
```

`backend/internal/work/doc.go`:
```go
// Package work owns Work Item records: the actual renovation work to be
// performed, the bridge between physical Spaces and the financial system.
// See phase1.md §8-9. Implementation begins in Milestone 2.
package work
```

`backend/internal/materials/doc.go`:
```go
// Package materials owns the Material Catalog and Material Reference
// Pricing. See phase1.md §10-11. Implementation begins in Milestone 3.
package materials
```

`backend/internal/suppliers/doc.go`:
```go
// Package suppliers owns the company-scoped Supplier Directory. See
// phase1.md §12. Implementation begins in Milestone 6.
package suppliers
```

`backend/internal/procurement/doc.go`:
```go
// Package procurement owns Material Requirements, RFQs, RFQ Invitations,
// and Supplier Offers — the procurement lifecycle from approved Work Items
// through Supplier pricing response. See phase1.md §32-39. Implementation
// begins in Milestone 6.
package procurement
```

`backend/internal/labour/doc.go`:
```go
// Package labour owns Worker records and Labour Entries (Project-specific
// labour cost tracking). See phase1.md §13-15. Implementation begins in
// Milestone 3.
package labour
```

`backend/internal/costs/doc.go`:
```go
// Package costs owns the CostItem model spanning Estimated, Committed,
// Actual, and Paid cost stages across Material, Labour, Subcontractor,
// Equipment, and other cost categories. See phase1.md §19, §40-41.
// Implementation begins in Milestone 3.
package costs
```

`backend/internal/estimates/doc.go`:
```go
// Package estimates owns the internal Estimate: the contractor's private
// financial calculation (costs, markup, margin, proposed selling price)
// that a Quotation is later generated from. See phase1.md §20-22.
// Implementation begins in Milestone 4.
package estimates
```

`backend/internal/quotations/doc.go`:
```go
// Package quotations owns the customer-facing Quotation: versioned,
// snapshotted selling-price documents generated from an Estimate, plus
// Client acceptance/rejection/change-request handling. See phase1.md
// §23-31. Implementation begins in Milestone 5.
package quotations
```

`backend/internal/payments/doc.go`:
```go
// Package payments owns Customer Payment tracking, kept separate from
// Project Cost tracking (profitability vs. cash flow). See phase1.md §42.
// Implementation begins in Milestone 7.
package payments
```

`backend/internal/documents/doc.go`:
```go
// Package documents owns Document metadata (Quotation PDFs, RFQ PDFs,
// Invoices, Photos, etc.), with physical file content delegated to
// platform/storage. See phase1.md §49. Implementation begins alongside
// the first module that generates a document (Milestone 5).
package documents
```

`backend/internal/approvals/doc.go`:
```go
// Package approvals owns the general Approval model (e.g. Client
// Quotation acceptance) that later extends to Architect/Engineer/
// Compliance review. See phase1.md §30-31. Implementation begins in
// Milestone 5.
package approvals
```

`backend/internal/audit/doc.go`:
```go
// Package audit owns Audit Events: an append-only record of critical
// business events (Project Created, Quotation Sent, RFQ Invitation Sent,
// etc.) layered on top of current-state domain records, not full event
// sourcing. See phase1.md §50. Implementation begins in Milestone 1 as
// other modules start producing events worth recording.
package audit
```

`backend/internal/ai/doc.go`:
```go
// Package ai owns domain-level AI Suggestion records (ai_suggestions
// collection: pending/accepted/modified/rejected) and the orchestration
// that turns authorized Project context into platform/ai.AIGateway calls.
// This package composes platform/ai; platform/ai holds no domain
// knowledge. See phase1.md §9, §18, §56, §61. Implementation begins in
// Milestone 8.
package ai
```

- [ ] **Step 2: Verify the module still builds with all placeholder packages present**

Run:
```powershell
cd backend
go build ./...
```

Expected: exits 0, no errors.

---

### Task 14: `platform/http` — router/API wiring helper

**Files:**
- Create: `backend/internal/platform/http/router.go`

- [ ] **Step 1: Implement the shared router/API constructor**

This centralizes chi+huma wiring so `cmd/api/main.go` stays a thin composition
point, and so Milestone 1+ modules register their routes the same way.

`backend/internal/platform/http/router.go`:
```go
// Package http centralizes chi router construction and Huma API mounting
// so every module registers HTTP operations the same way. Business
// services must not depend on chi or huma types — those stay at this
// boundary and in each module's own handler.go.
package http

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter constructs a chi.Router with baseline cross-cutting middleware
// (request ID, panic recovery) and a Huma API mounted at its root, ready
// for domain modules to register operations against.
func NewRouter(apiTitle, apiVersion string) (chi.Router, huma.API) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)

	api := humachi.New(router, huma.DefaultConfig(apiTitle, apiVersion))

	return router, api
}
```

- [ ] **Step 2: Add the chi middleware dependency (already transitively available via chi, but pin explicitly)**

Run:
```powershell
cd backend
go get github.com/go-chi/chi/v5@latest
go build ./...
```

Expected: exits 0. (`chi/v5/middleware` ships in the same module as `chi/v5`, so no separate `go get` is required — this step just confirms the build succeeds.)

---

### Task 15: `/health` and `/ready` endpoints + `cmd/api/main.go`

**Files:**
- Create: `backend/internal/platform/http/health.go`
- Create: `backend/cmd/api/main.go`
- Test: `backend/internal/platform/http/health_test.go`

- [ ] **Step 1: Write the failing test for `/health` and `/ready`**

`backend/internal/platform/http/health_test.go`:
```go
package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func TestHealthEndpointReturns200WithoutDependencies(t *testing.T) {
	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, nil) // nil client: /health must not touch Mongo

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReadyEndpointReturns200WhenMongoIsUp(t *testing.T) {
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = platformmongo.Disconnect(context.Background(), client) }()

	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, client)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReadyEndpointReturns503WhenMongoIsUnreachable(t *testing.T) {
	client, err := platformmongo.Connect("mongodb://localhost:1/?connectTimeoutMS=500&serverSelectionTimeoutMS=500")
	if err != nil {
		t.Fatalf("Connect itself should not fail (lazy connection): %v", err)
	}
	defer func() { _ = platformmongo.Disconnect(context.Background(), client) }()

	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, client)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```powershell
cd backend
go test ./internal/platform/http/... -v
```

Expected: FAIL — `platformhttp.RegisterHealth` undefined.

- [ ] **Step 3: Implement `/health` and `/ready`**

`backend/internal/platform/http/health.go`:
```go
package http

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.mongodb.org/mongo-driver/v2/mongo"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

// HealthOutput is the response body for /health and /ready.
type HealthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"ok or unavailable"`
	}
}

// RegisterHealth registers /health (process liveness; never touches Mongo)
// and /ready (verifies Mongo connectivity) operations on api. mongoClient
// may be nil, in which case /ready always reports unavailable.
func RegisterHealth(api huma.API, mongoClient *mongo.Client) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Process liveness check",
		Tags:        []string{"System"},
	}, func(_ context.Context, _ *struct{}) (*HealthOutput, error) {
		resp := &HealthOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-ready",
		Method:      http.MethodGet,
		Path:        "/ready",
		Summary:     "Readiness check (verifies MongoDB connectivity)",
		Tags:        []string{"System"},
	}, func(ctx context.Context, _ *struct{}) (*HealthOutput, error) {
		if mongoClient == nil {
			return nil, huma.Error503ServiceUnavailable("mongodb client not configured")
		}
		if err := platformmongo.Ping(ctx, mongoClient); err != nil {
			return nil, huma.Error503ServiceUnavailable("mongodb unreachable", err)
		}

		resp := &HealthOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```powershell
go test ./internal/platform/http/... -v
```

Expected: PASS (all three tests).

- [ ] **Step 5: Write `cmd/api/main.go`**

`backend/cmd/api/main.go`:
```go
// Command api is the entrypoint for the Renovation Project Intelligence
// Platform backend: loads configuration, connects to MongoDB, wires the
// HTTP router, and serves the API.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/platform/logging"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func main() {
	// Loading a .env file is best-effort; in production config comes from
	// real environment variables and no .env file is expected to exist.
	_ = godotenv.Load()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		panic(err)
	}

	logger := logging.New(os.Stdout, "info")
	logger.Info().Str("env", cfg.AppEnv).Msg("starting api")

	mongoClient, err := platformmongo.Connect(cfg.MongoURI)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to construct mongodb client")
	}
	defer func() {
		if err := platformmongo.Disconnect(context.Background(), mongoClient); err != nil {
			logger.Error().Err(err).Msg("failed to disconnect mongodb client")
		}
	}()

	router, api := platformhttp.NewRouter("Renovation Project Intelligence API", "0.1.0")
	platformhttp.RegisterHealth(api, mongoClient)

	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info().Str("port", cfg.HTTPPort).Msg("listening")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal().Err(err).Msg("server failed")
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info().Msg("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("graceful shutdown failed")
	}
}
```

- [ ] **Step 6: Verify the binary builds**

Run:
```powershell
cd backend
go build ./...
```

Expected: exits 0.

- [ ] **Step 7: Manually verify against the running Docker Compose Mongo**

Run (ensure `docker compose up -d` from Task 2 is still running):
```powershell
cd backend
Copy-Item ../.env.example ../.env -Force
$env:MONGO_URI = "mongodb://localhost:27017"
$env:MONGO_DATABASE = "renovation_platform"
Start-Process -NoNewWindow go -ArgumentList "run", "./cmd/api"
Start-Sleep -Seconds 3
Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing | Select-Object -ExpandProperty StatusCode
Invoke-WebRequest -Uri "http://localhost:8080/ready" -UseBasicParsing | Select-Object -ExpandProperty StatusCode
```

Expected: both return `200`. Stop the background process afterward:
```powershell
Get-Process go -ErrorAction SilentlyContinue | Stop-Process -Force
```

(If `Start-Process`/background management is awkward in this shell, run `go run ./cmd/api` in one terminal and issue the two `Invoke-WebRequest` calls from another, then `Ctrl+C` the server.)

---

### Task 16: Backend full-suite verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full backend test suite**

Run:
```powershell
cd backend
go test ./... -v
```

Expected: PASS for every package (`foundation/money`, `foundation/quantity`,
`platform/config`, `platform/logging`, `platform/mongo`, `platform/storage`,
`platform/mail`, `platform/jobs`, `platform/ai`, `platform/http`). Domain
module placeholder packages have no tests and are skipped (no test files),
which is expected.

- [ ] **Step 2: Run `go vet`**

Run:
```powershell
go vet ./...
```

Expected: no output, exit 0.

- [ ] **Step 3: Confirm `go.mod`/`go.sum` are fully tidy**

Run:
```powershell
go mod tidy
```

Expected: `go mod tidy` produces no changes to `go.mod`/`go.sum` (running it twice in a row is a no-op once dependencies are settled).

---

### Task 17: Next.js frontend shell

**Files:**
- Create: `apps/web/` (via `create-next-app`)

- [ ] **Step 1: Scaffold the Next.js app**

Run (from repo root):
```powershell
cd apps
npx create-next-app@latest web --typescript --eslint --app --src-dir --import-alias "@/*" --use-npm --no-tailwind
```

When prompted interactively (if any prompt appears despite the flags), accept the defaults matching the flags above.

Expected: `apps/web/` created with a standard Next.js App Router + TypeScript + ESLint project.

- [ ] **Step 2: Replace the default home page with a minimal placeholder**

Read the generated `apps/web/src/app/page.tsx` first, then replace its contents:

`apps/web/src/app/page.tsx`:
```tsx
export default function Home() {
  return (
    <main style={{ padding: "2rem", fontFamily: "system-ui, sans-serif" }}>
      <h1>Renovation Project Intelligence Platform</h1>
      <p>Milestone 0 scaffold — frontend shell.</p>
    </main>
  );
}
```

- [ ] **Step 3: Verify the dev server starts and serves the placeholder**

Run:
```powershell
cd apps/web
npm run build
```

Expected: build completes with no errors (`✓ Compiled successfully`).

- [ ] **Step 4: Verify `npm run dev` serves the page (manual check)**

Run:
```powershell
cd apps/web
Start-Process -NoNewWindow npm -ArgumentList "run", "dev"
Start-Sleep -Seconds 5
Invoke-WebRequest -Uri "http://localhost:3000" -UseBasicParsing | Select-Object -ExpandProperty StatusCode
```

Expected: `200`. Stop the dev server afterward:
```powershell
Get-Process node -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowTitle -eq "" } | Stop-Process -Force
```

(If background process cleanup is unreliable in this shell, run `npm run dev` in a foreground terminal, check `http://localhost:3000` in a browser or via a second terminal's `Invoke-WebRequest`, then `Ctrl+C`.)

---

### Task 18: Architectural Decision Records

**Files:**
- Create: `docs/adr/0001-money-and-decimal-strategy.md`
- Create: `docs/adr/0002-modular-monolith-module-boundaries.md`
- Create: `docs/adr/0003-local-first-infrastructure.md`

- [ ] **Step 1: Write ADR 0001**

`docs/adr/0001-money-and-decimal-strategy.md`:
```markdown
# ADR 0001: Money and Decimal Strategy

## Status
Accepted

## Context
Financial correctness is a hard requirement (phase1.md §51). Binary
floating-point (float32/float64) cannot represent decimal currency amounts
exactly and must not be used for authoritative financial or quantity
calculations.

## Decision
- Canonical Money is `int64` minor units + currency code
  (`internal/foundation/money.Money`), e.g. RM18.50 = `{1850, "MYR"}`.
- Rates/percentages are fixed-point `RateBPS` (basis points), not floats.
- Construction quantities use `shopspring/decimal`
  (`internal/foundation/quantity.Quantity`), not `float64`.
- Calculation flow: decimal Quantity × Money unit price (as decimal) →
  precise decimal intermediate result → centralized rounding
  (`money.RoundToMinorUnits`, round-half-up to the nearest minor unit) →
  final `int64` Money.
- MongoDB Decimal128 is not used as the default Money representation in
  Phase 1.
- Money operations validate currency compatibility (adding MYR to SGD is
  an error).

## Consequences
- All modules share one rounding implementation; no per-module rounding
  logic.
- Display/formatting layers (API responses, frontend) are responsible for
  converting minor units to a human-readable major-unit string; that
  conversion is presentation-only and never feeds back into authoritative
  calculations.
```

- [ ] **Step 2: Write ADR 0002**

`docs/adr/0002-modular-monolith-module-boundaries.md`:
```markdown
# ADR 0002: Modular Monolith Module Boundaries

## Status
Accepted

## Context
Phase 1 is one Go application (a modular monolith), not microservices
(phase1.md §63). Without enforced boundaries, a monolith tends toward
tangled cross-module database access over time.

## Decision
- Each domain module (`identity`, `projects`, `quotations`, etc.) owns its
  own model, repository interface + MongoDB implementation, service, and
  Huma HTTP handlers.
- A module must never directly access another module's MongoDB repository
  or collection.
- Cross-module interaction happens only through narrow, explicitly defined
  capability interfaces exposed by the owning module's Service.
- Huma request/response DTOs stay at the HTTP boundary; handlers convert
  them to domain/application commands before invoking a Service.
- `internal/foundation` (Money, Quantity — business primitives) has no
  dependency on `internal/platform` or any domain module.
- `internal/platform` (Mongo, config, logging, storage, mail, jobs, AI
  gateway — technical infrastructure) has no dependency on domain modules
  and exposes no generic cross-domain CRUD repository.

Dependency direction: Chi Router → Huma API → Module Handler → Module
Service → Repository Interface → MongoDB Repository. Cross-module: Module
A Service → narrow capability interface → Module B Service.

## Consequences
- Slightly more boilerplate per module (each gets its own repository
  rather than sharing a generic one) in exchange for boundaries that hold
  as the codebase grows and stay compatible with the Go module structure
  Phase 1 has now.
- A module could later be extracted into a separate service without a
  data-access rewrite, if that ever becomes justified.
```

- [ ] **Step 3: Write ADR 0003**

`docs/adr/0003-local-first-infrastructure.md`:
```markdown
# ADR 0003: Local-First Infrastructure

## Status
Accepted

## Context
The application must run entirely on a developer machine with no AWS
account and no external cloud dependency, while still being designed so
managed/cloud implementations can be substituted later without rewriting
business logic.

## Decision

| Concern | Phase 1 (local) | Future |
|---|---|---|
| Database | MongoDB via Docker Compose | MongoDB Atlas |
| File storage | Local filesystem (`./data/uploads/`) behind `FileStorage` | S3 (`S3FileStorage`) |
| Email | Mailpit (local SMTP catcher) behind `EmailSender` | Managed transactional email |
| Background jobs | In-process worker behind `JobQueue`, non-durable | SQS-backed queue |
| Auth | Local JWT (access + refresh pair), bcrypt hashing — implemented in Milestone 1 | Managed identity provider |
| AI | `AIGateway` interface, `AI_PROVIDER=mock` default | Real LLM/multimodal provider |

No AWS SDK dependencies are added while there is no functioning local
alternative behind the same interface. `AI_PROVIDER=mock` is the default
so the application is fully testable without external AI API credentials.

## Consequences
- Onboarding a new developer requires only Docker, Go, and Node — no cloud
  account.
- Swapping any of the above for a managed implementation later means
  writing a new implementation of an existing interface, not touching
  domain modules.
- Non-durable in-process jobs mean queued work is lost on process restart
  in Phase 1; acceptable for local development, revisit before any
  production deployment that requires durability guarantees.
```

---

### Task 19: Final Milestone 0 acceptance check

**Files:** none (verification only)

- [ ] **Step 1: Full clean-slate verification**

Run from repo root:
```powershell
docker compose down
docker compose up -d
Start-Sleep -Seconds 5
docker compose ps
```

Expected: `renovation-mongo` and `renovation-mailpit` both `running`.

- [ ] **Step 2: Backend tests**

Run:
```powershell
cd backend
go build ./...
go vet ./...
go test ./...
```

Expected: all exit 0.

- [ ] **Step 3: Backend serves health/ready against the compose Mongo**

Run:
```powershell
Copy-Item ../.env.example ../.env -Force
go run ./cmd/api
```

In a second terminal:
```powershell
Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing | Select-Object -ExpandProperty StatusCode
Invoke-WebRequest -Uri "http://localhost:8080/ready" -UseBasicParsing | Select-Object -ExpandProperty StatusCode
```

Expected: both `200`. Stop the server with `Ctrl+C` in the first terminal.

- [ ] **Step 4: Frontend serves**

Run:
```powershell
cd ../apps/web
npm run build
```

Expected: exits 0.

- [ ] **Step 5: Confirm this matches the design doc's acceptance criteria**

Re-read `docs/superpowers/specs/2026-07-19-phase1-foundation-design.md` §10
"Acceptance criteria" and confirm each bullet is satisfied:
- `docker compose up -d` starts Mongo + Mailpit — confirmed Step 1.
- `go run ./cmd/api` connects to Mongo and serves `/health`/`/ready` (200) — confirmed Step 3.
- `go test ./...` passes — confirmed Step 2.
- `npm run dev` in `apps/web` serves a blank Next.js shell — confirmed Task 17 Step 4, rebuilt Step 4 here.

Milestone 0 is complete. This project is kept fully local (no git repository) per current instructions — nothing further to do here.
