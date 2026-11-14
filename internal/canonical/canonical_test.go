package canonical

import (
	"math"
	"strings"
	"testing"
)

func TestDecimal12(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input float64
		want  string
	}{
		{input: 0, want: "0.000000000000"},
		{input: math.Copysign(0, -1), want: "0.000000000000"},
		{input: 0.00000001, want: "0.000000010000"},
		{input: 123.5, want: "123.500000000000"},
	}
	for _, test := range tests {
		got, err := Decimal12(test.input)
		if err != nil {
			t.Fatalf("Decimal12(%v): %v", test.input, err)
		}
		if got != test.want {
			t.Errorf("Decimal12(%v) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestJSONSortsKeysAndEndsWithLF(t *testing.T) {
	t.Parallel()

	got, err := JSON(map[string]any{"z": 1, "a": map[string]any{"y": true, "b": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":{\"b\":\"x\",\"y\":true},\"z\":1}\n"
	if string(got) != want {
		t.Fatalf("JSON() = %q, want %q", got, want)
	}
	if strings.Contains(string(got), "  ") {
		t.Fatal("canonical JSON must not be indented")
	}
}

func TestJSONNormalizesUnicodeNFC(t *testing.T) {
	t.Parallel()

	precomposed, err := JSON(map[string]any{"name": "caf\u00e9"})
	if err != nil {
		t.Fatal(err)
	}
	decomposed, err := JSON(map[string]any{"name": "cafe\u0301"})
	if err != nil {
		t.Fatal(err)
	}
	if string(precomposed) != string(decomposed) {
		t.Fatalf("NFC mismatch: %q != %q", precomposed, decomposed)
	}
}

func TestJSONRejectsKeysThatCollideAfterNFC(t *testing.T) {
	t.Parallel()

	_, err := JSON(map[string]any{"caf\u00e9": 1, "cafe\u0301": 2})
	if err == nil {
		t.Fatal("colliding normalized keys were accepted")
	}
}

func TestCanonicalErrorPathsAndNestedNormalization(t *testing.T) {
	t.Parallel()

	if _, err := Decimal12(math.Inf(1)); err == nil {
		t.Fatal("infinite decimal accepted")
	}
	if _, err := DecimalString12("not-a-number"); err == nil {
		t.Fatal("invalid decimal accepted")
	}
	if _, err := JSON(make(chan int)); err == nil {
		t.Fatal("unsupported JSON value accepted")
	}
	value, err := JSON([]any{"cafe\u0301", map[string]any{"html": "<x>"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "[\"caf\u00e9\",{\"html\":\"<x>\"}]\n" {
		t.Fatalf("nested canonical JSON = %q", value)
	}
}

func TestDecimalString12UsesHalfEvenWithoutBinaryArtifacts(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"28923.63":         "28923.630000000000",
		"1.2345678901245":  "1.234567890124",
		"1.2345678901255":  "1.234567890126",
		"-0.0000000000004": "0.000000000000",
	}
	for input, want := range tests {
		got, err := DecimalString12(input)
		if err != nil {
			t.Fatalf("DecimalString12(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("DecimalString12(%q) = %q, want %q", input, got, want)
		}
	}
}
