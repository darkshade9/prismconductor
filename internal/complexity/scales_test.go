package complexity

import (
	"strings"
	"testing"
)

func TestIsValid_TShirt(t *testing.T) {
	valid := []string{"XS", "S", "M", "L", "XL"}
	for _, v := range valid {
		if !IsValid(ScaleTShirt, v) {
			t.Errorf("IsValid(%q, %q) = false, want true", ScaleTShirt, v)
		}
	}
	invalid := []string{"small", "medium", "large", "xs", "1", "2", "3", "5", "8", "?"}
	for _, v := range invalid {
		if IsValid(ScaleTShirt, v) {
			t.Errorf("IsValid(%q, %q) = true, want false", ScaleTShirt, v)
		}
	}
}

func TestIsValid_Empty_DefaultsTShirt(t *testing.T) {
	if !IsValid("", "M") {
		t.Error("empty scale should default to tshirt; IsValid(\"\", \"M\") = false")
	}
	if IsValid("", "small") {
		t.Error("empty scale should default to tshirt; IsValid(\"\", \"small\") = true for raw legacy value")
	}
}

func TestIsValid_Fibonacci(t *testing.T) {
	valid := []string{"1", "2", "3", "5", "8", "13", "21", "?"}
	for _, v := range valid {
		if !IsValid(ScaleFibonacci, v) {
			t.Errorf("IsValid(%q, %q) = false, want true", ScaleFibonacci, v)
		}
	}
	invalid := []string{"XS", "S", "M", "L", "XL", "4", "6", "10"}
	for _, v := range invalid {
		if IsValid(ScaleFibonacci, v) {
			t.Errorf("IsValid(%q, %q) = true, want false", ScaleFibonacci, v)
		}
	}
}

func TestIsValid_Linear10(t *testing.T) {
	valid := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	for _, v := range valid {
		if !IsValid(ScaleLinear10, v) {
			t.Errorf("IsValid(%q, %q) = false, want true", ScaleLinear10, v)
		}
	}
	invalid := []string{"XS", "S", "M", "L", "XL", "0", "11", "?"}
	for _, v := range invalid {
		if IsValid(ScaleLinear10, v) {
			t.Errorf("IsValid(%q, %q) = true, want false", ScaleLinear10, v)
		}
	}
}

func TestNormalize_TShirt_LegacyValues(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"small", "S"},
		{"Small", "S"},
		{"SMALL", "S"},
		{"medium", "M"},
		{"large", "L"},
		{"xs", "XS"},
		{"extra-small", "XS"},
		{"xl", "XL"},
		{"extra-large", "XL"},
	}
	for _, c := range cases {
		got, err := Normalize(ScaleTShirt, c.raw)
		if err != nil {
			t.Errorf("Normalize(%q, %q) err = %v", ScaleTShirt, c.raw, err)
		}
		if got != c.want {
			t.Errorf("Normalize(%q, %q) = %q, want %q", ScaleTShirt, c.raw, got, c.want)
		}
	}
}

func TestNormalize_TShirt_ValidPassthrough(t *testing.T) {
	for _, v := range tshirtValues {
		got, err := Normalize(ScaleTShirt, v)
		if err != nil {
			t.Errorf("Normalize(%q, %q) err = %v", ScaleTShirt, v, err)
		}
		if got != v {
			t.Errorf("Normalize(%q, %q) = %q, want %q", ScaleTShirt, v, got, v)
		}
	}
}

func TestNormalize_TShirt_UnrecognizedReturnsError(t *testing.T) {
	_, err := Normalize(ScaleTShirt, "complicated")
	if err == nil {
		t.Error("expected error for unrecognized tshirt value")
	}
}

func TestNormalize_Fibonacci_ValidPassthrough(t *testing.T) {
	for _, v := range fibonacciValues {
		got, err := Normalize(ScaleFibonacci, v)
		if err != nil {
			t.Errorf("Normalize(%q, %q) err = %v", ScaleFibonacci, v, err)
		}
		if got != v {
			t.Errorf("Normalize(%q, %q) = %q, want %q", ScaleFibonacci, v, got, v)
		}
	}
}

func TestNormalize_Fibonacci_LegacyNotMapped(t *testing.T) {
	_, err := Normalize(ScaleFibonacci, "small")
	if err == nil {
		t.Error("expected error: legacy tshirt values should not normalize for fibonacci scale")
	}
}

func TestPromptFragment_ContainsScaleValues(t *testing.T) {
	// Note: fragments intentionally mention other scales in "do not use" guidance,
	// so mustNotContain only checks strings that are uniquely absent.
	cases := []struct {
		scale       string
		mustContain []string
	}{
		{
			scale:       ScaleTShirt,
			mustContain: []string{"XS", "S", "M", "L", "XL", "T-shirt"},
		},
		{
			scale:       ScaleFibonacci,
			mustContain: []string{"1", "2", "3", "5", "8", "13", "21", "?", "Fibonacci"},
		},
		{
			scale:       ScaleLinear10,
			mustContain: []string{"1", "10", "Linear"},
		},
		{
			scale:       "", // empty defaults to tshirt
			mustContain: []string{"XS", "T-shirt"},
		},
	}
	for _, c := range cases {
		frag := PromptFragment(c.scale)
		for _, s := range c.mustContain {
			if !strings.Contains(frag, s) {
				t.Errorf("PromptFragment(%q): missing %q", c.scale, s)
			}
		}
	}
}
