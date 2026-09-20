package auth

import "testing"

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Fatal("expected short fail")
	}
	if err := ValidatePassword("onlyletters"); err == nil {
		t.Fatal("expected digit fail")
	}
	if err := ValidatePassword("ChangeMe123!"); err != nil {
		t.Fatal(err)
	}
}
