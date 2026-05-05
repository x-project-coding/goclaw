//go:build integration

package integration

import (
	"os/exec"
	"testing"
)

// TestFoundation_NoTenantResidue is a compile-time + runtime regression gate
// that ensures no live tenant_id / TenantID / tenantCond / WithTenantID code
// survives in internal/, migrations/, or pkg/. It delegates to the canonical
// shell script so the grep patterns stay in one place and both CI and the test
// suite always agree.
func TestFoundation_NoTenantResidue(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("bash", "scripts/check-tenant-purge.sh")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tenant residue detected:\n%s", out)
	}
}
