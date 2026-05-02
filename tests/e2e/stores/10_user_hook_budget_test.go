//go:build e2e

package stores_test

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestUserHookBudgetMonthlyReset verifies:
//  1. Upsert creates a row when missing, updates when present.
//  2. ResetMonthly rolls month_start forward and resets remaining=budget_total.
//  3. Per-user isolation — user A budget changes do not affect user B.
func TestUserHookBudgetMonthlyReset(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	users := pg.NewPGUsersStore(helpers.MustDB(t))
	budget := pg.NewPGUserHookBudgetStore(helpers.MustDB(t))

	uA := &store.User{Email: helpers.RandEmail("budgetA"), PasswordHash: "x", Role: "member", Status: "active"}
	uB := &store.User{Email: helpers.RandEmail("budgetB"), PasswordHash: "x", Role: "member", Status: "active"}
	if err := users.Create(ctx, uA); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := users.Create(ctx, uB); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	may := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// Upsert May budget for both users.
	if err := budget.Upsert(ctx, &store.UserHookBudget{
		UserID: uA.ID, MonthStart: may, BudgetTotal: 1000, Remaining: 1000,
	}); err != nil {
		t.Fatalf("Upsert A May: %v", err)
	}
	if err := budget.Upsert(ctx, &store.UserHookBudget{
		UserID: uB.ID, MonthStart: may, BudgetTotal: 500, Remaining: 500,
	}); err != nil {
		t.Fatalf("Upsert B May: %v", err)
	}

	// Decrement A by 250 — B unaffected.
	if err := budget.Decrement(ctx, uA.ID, 250); err != nil {
		t.Fatalf("Decrement A: %v", err)
	}
	a, err := budget.Get(ctx, uA.ID)
	if err != nil {
		t.Fatalf("Get A: %v", err)
	}
	if a.Remaining != 750 {
		t.Fatalf("Decrement A: want remaining=750, got %d", a.Remaining)
	}
	b, err := budget.Get(ctx, uB.ID)
	if err != nil {
		t.Fatalf("Get B: %v", err)
	}
	if b.Remaining != 500 {
		t.Fatalf("isolation broken: B remaining=%d (expected 500)", b.Remaining)
	}

	// ResetMonthly to June — month_start moves, remaining = budget_total.
	if err := budget.ResetMonthly(ctx, uA.ID, june, 2000); err != nil {
		t.Fatalf("ResetMonthly A: %v", err)
	}
	a, _ = budget.Get(ctx, uA.ID)
	if !a.MonthStart.Equal(june) {
		t.Fatalf("ResetMonthly: month_start want %v, got %v", june, a.MonthStart)
	}
	if a.BudgetTotal != 2000 || a.Remaining != 2000 {
		t.Fatalf("ResetMonthly: want budget=remaining=2000, got total=%d remaining=%d",
			a.BudgetTotal, a.Remaining)
	}

	// Get for unknown user returns ErrNotFound.
	uC := &store.User{Email: helpers.RandEmail("nope"), PasswordHash: "x", Role: "member", Status: "active"}
	if err := users.Create(ctx, uC); err != nil {
		t.Fatalf("seed C: %v", err)
	}
	if _, err := budget.Get(ctx, uC.ID); err == nil {
		t.Fatalf("Get for user without budget: want ErrNotFound, got nil")
	}
}
