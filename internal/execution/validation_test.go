package execution

import "testing"

func TestValidateM2(t *testing.T) {
	t.Parallel()

	report, err := ValidateM2()
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" || len(report.Errors) != 0 || report.Days != 5 {
		t.Fatalf("unexpected validation report: %+v", report)
	}
	for name, passed := range report.Checks {
		if !passed {
			t.Errorf("validation check %s failed", name)
		}
	}
}
