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

type Priced interface {
	Minor() int64
	Currency() string
}

type Points int64

func (p Points) WithCurrency(code string) Money { return Money{int64(p), code} }

type Wrapper struct{ Money }

type Quote struct{ Total Priced }

type Reward struct{ Bonus Points }

type Bundle struct{ Total Wrapper }

type Surcharge struct {
	Money
	Fee int64
}

type Settlement struct {
	Amount             int64
	SettlementCurrency string
}

type Batch struct{ Amounts []int64 }

type Inline struct {
	Total struct {
		V        int64
		Currency string
	}
}
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
		{"Embedded", "Amount", fact.CurrencyMissing, "Base is embedded and was read: it has no currency field"},
		{"Opaque", "Amount", "", "an interface could hold anything"},
		{"Quote", "Total", fact.CurrencyInType, "Priced is an interface with a Currency method"},
		{"Reward", "Bonus", fact.CurrencyMissing, "WithCurrency takes a currency in, so Points has none"},
		{"Bundle", "Total", fact.CurrencyInType, "Wrapper embeds Money, so Wrapper.Currency is promoted"},
		{"Surcharge", "Fee", fact.CurrencyBeside, "Money is embedded, so Surcharge.Currency is promoted"},
		{"Settlement", "Amount", fact.CurrencyBeside, "SettlementCurrency sits beside it"},
		{"Batch", "Amounts", fact.CurrencyMissing, "a slice of int64 cannot hold a currency"},
		{"example.com/m/domain", "Cents", fact.CurrencyInType, "a named money type is read too"},
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
	cs := moneyLab(t).MoneyTypes()
	for _, tc := range []struct{ owner, proof string }{
		{"Order", "in Money.Currency"},
		{"Bundle", "in Wrapper.Currency"},
		{"Inline", "carries a currency in Currency"},
	} {
		c, ok := cs.Find(tc.owner, "Total")
		if !ok {
			t.Errorf("%s.Total: not a money candidate", tc.owner)
			continue
		}
		if len(c.Against) != 0 || !strings.Contains(strings.Join(c.Proof, "\n"), tc.proof) {
			t.Errorf("%s.Total: proof %q, against %q: want %q as evidence for, not against", tc.owner, c.Proof, c.Against, tc.proof)
		}
	}
}

func TestACurrencyCodeIsNotAnAmount(t *testing.T) {
	if c, ok := moneyLab(t).MoneyTypes().Find("Settlement", "SettlementCurrency"); ok {
		t.Errorf("SettlementCurrency is a currency, not money: %+v", c)
	}
}

func TestNoCurrencyFindingsAgreeWithTheFact(t *testing.T) {
	p := moneyLab(t)
	cs := p.MoneyTypes()
	fired := 0
	for _, f := range p.Findings() {
		if f.ID != "MONEY-NO-CURRENCY" {
			continue
		}
		fired++
		owner, name, _ := strings.Cut(strings.Fields(f.Title)[0], ".")
		if c, _ := cs.Find(owner, name); c.Currency != fact.CurrencyMissing {
			t.Errorf("%s fires, but the fact says currency = %q", f.Title, c.Currency)
		}
	}
	if fired == 0 {
		t.Fatal("MONEY-NO-CURRENCY never fired, so nothing was compared")
	}
}

func TestUnknownCurrencyIsLeftOutOfTheJSON(t *testing.T) {
	cs := moneyLab(t).MoneyTypes()
	known, ok1 := cs.Find("Fees", "Fee")
	unknown, ok2 := cs.Find("Opaque", "Amount")
	if !ok1 || !ok2 {
		t.Fatal("Fees.Fee and Opaque.Amount must both be money candidates")
	}
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
