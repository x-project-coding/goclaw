package tools

import (
	"context"
	"testing"
)

func TestSkillServiceEnvMap_CRMAPIDefault(t *testing.T) {
	t.Setenv("CRM_SERVICE_BASE_URL", "")
	m := skillServiceEnvMap(runCtx())
	if m == nil {
		t.Fatal("expected an env map for a run context, got nil")
	}
	if got, want := m["CRM_API"], "https://crm.42bucks.com"; got != want {
		t.Fatalf("CRM_API default = %q, want %q", got, want)
	}
}

func TestSkillServiceEnvMap_CRMAPIOverride(t *testing.T) {
	t.Setenv("CRM_SERVICE_BASE_URL", "https://crm.example.test/")
	m := skillServiceEnvMap(runCtx())
	if got, want := m["CRM_API"], "https://crm.example.test"; got != want {
		t.Fatalf("CRM_API override = %q, want %q (trailing slash trimmed)", got, want)
	}
}

func TestSkillServiceEnvMap_NoRunContext(t *testing.T) {
	if m := skillServiceEnvMap(context.Background()); m != nil {
		t.Fatalf("expected nil env map without a run context, got %v", m)
	}
}
