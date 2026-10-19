package scan

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/katayunak/gofacts/fact"
)

func moneyLab(t *testing.T) *Program {
	return lab(t, "money", func() map[string]string {
		return map[string]string{
			"domain/money.go": `package domain

type Money struct {
	Minor    int64
	Currency string
}

type Cents int64

func (Cents) Currency() string { return "EUR" }

type Plain struct{ Units int64 }

type Base struct{ Code string }

type Valuer interface{ Value() int64 }

type Charge struct {
	Amount   int64
	Currency string
}

type Order struct {
	Total Money
	Refund *Money
}

type Invoice struct{ Price Cents }

type Fees struct {
	Fee      int64
	Discount float64
	Tax      Plain
}

type Embedded struct {
	Base
	Amount int64
}

type Opaque struct{ Amount Valuer }
`,
		}
	})
}

func TestMoneyCandidatesSayWhereTheCurrencyIs(t *testing.T) {
	cs := moneyLab(t).MoneyTypes()
	for _, tc := range []struct {
		owner, name string
		want        fact.CurrencyPlace
		why         string
	}{
		{"Charge", "Amount", fact.CurrencyBeside, "Currency is a field of the same struct"},
		{"Money", "Minor", fact.CurrencyBeside, "Currency is a field of the same struct"},
		{"Order", "Total", fact.CurrencyInType, "Money has a Currency field"},
		{"Order", "Refund", fact.CurrencyInType, "a pointer to Money still carries its Currency"},
		{"Invoice", "Price", fact.CurrencyInType, "Cents has a Currency method"},
		{"Fees", "Fee", fact.CurrencyMissing, "a bare int64 with nothing beside it"},
		{"Fees", "Discount", fact.CurrencyMissing, "a bare float64 with nothing beside it"},
		{"Fees", "Tax", fact.CurrencyMissing, "Plain was read and has no currency field"},
		{"Embedded", "Amount", "", "Base is embedded, so a promoted field could hold the currency"},
		{"Opaque", "Amount", "", "an interface could hold anything"},
	} {
		c, ok := cs.Find(tc.owner, tc.name)
		if !ok {
			t.Errorf("%s.%s: not a money candidate", tc.owner, tc.name)
			continue
		}
		if c.Currency != tc.want {
			t.Errorf("%s.%s: currency = %q, want %q (%s); proof %q, against %q", tc.owner, tc.name, c.Currency, tc.want, tc.why, c.Proof, c.Against)
		}
	}
}

func TestACurrencyInTheAmountsTypeCountsAsEvidence(t *testing.T) {
	c, ok := moneyLab(t).MoneyTypes().Find("Order", "Total")
	if !ok {
		t.Fatal("Order.Total: not a money candidate")
	}
	if len(c.Against) != 0 || !strings.Contains(strings.Join(c.Proof, "\n"), "Money.Currency") {
		t.Errorf("proof %q, against %q: the currency inside Money is evidence for, not against", c.Proof, c.Against)
	}
}

func TestUnknownCurrencyIsLeftOutOfTheJSON(t *testing.T) {
	cs := moneyLab(t).MoneyTypes()
	known, _ := cs.Find("Fees", "Fee")
	unknown, _ := cs.Find("Opaque", "Amount")
	k, _ := json.Marshal(known)
	u, _ := json.Marshal(unknown)
	if !strings.Contains(string(k), `"currency":"missing"`) {
		t.Errorf("a missing currency must be written out: %s", k)
	}
	if strings.Contains(string(u), `"currency"`) {
		t.Errorf("not known must not be written as a value: %s", u)
	}
}

func TestPaysvcFeeHasNoCurrency(t *testing.T) {
	c, ok := fixture(t, "paysvc").MoneyTypes().Find("Payment", "FeeCents")
	if !ok || c.Currency != fact.CurrencyMissing {
		t.Fatalf("Payment.FeeCents = %+v; MONEY-NO-CURRENCY fires on it, so the fact must agree", c)
	}
}
