package llmout

import (
	"errors"
	"strings"
	"testing"
)

type answer struct {
	Field string `json:"field"`
}

func TestFencesAreStripped(t *testing.T) {
	for _, in := range []string{
		"```json\n{\"field\":\"a\"}\n```",
		"```\n{\"field\":\"a\"}\n```\n",
		"  {\"field\":\"a\"}  ",
	} {
		var a answer
		if err := DecodeStrict([]byte(in), &a); err != nil || a.Field != "a" {
			t.Errorf("%q: %+v %v", in, a, err)
		}
	}
}

func TestStrictRejectsFieldsNobodyAskedFor(t *testing.T) {
	var a answer
	if err := DecodeStrict([]byte(`{"field":"a","feild":"b"}`), &a); err == nil {
		t.Fatal("a misspelt field must not be dropped silently")
	}
	if err := Decode([]byte(`{"field":"a","extra":1}`), &a); err != nil {
		t.Fatalf("lenient decode: %v", err)
	}
}

func TestProseAfterTheAnswerIsRejected(t *testing.T) {
	var a answer
	if err := Decode([]byte("{\"field\":\"a\"}\nI hope this helps!"), &a); !errors.Is(err, ErrTrailing) {
		t.Fatalf("err = %v", err)
	}
	if err := Decode([]byte(`{"field":"a"} {"field":"b"}`), &a); !errors.Is(err, ErrTrailing) {
		t.Fatalf("two values: err = %v", err)
	}
}

func TestOversizedAnswersAreRefused(t *testing.T) {
	var a answer
	big := `{"field":"` + strings.Repeat("x", MaxBytes) + `"}`
	if err := Decode([]byte(big), &a); err == nil {
		t.Fatal("expected a size error")
	}
}
