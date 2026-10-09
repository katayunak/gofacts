package naming

import (
	"slices"
	"testing"
)

func TestWordsSplitsGoIdentifiers(t *testing.T) {
	for in, want := range map[string][]string{
		"TotalPages":         {"total", "pages"},
		"total_pages":        {"total", "pages"},
		"CurrentPage":        {"current", "page"},
		"Price":              {"price"},
		"HTTPStatus":         {"http", "status"},
		"ReverseDiscountJob": {"reverse", "discount", "job"},
		"amount2Cents":       {"amount2", "cents"},
		"IdempotencyKey":     {"idempotency", "key"},
		"x-idempotency-key":  {"x", "idempotency", "key"},
	} {
		if got := Words(in); !slices.Equal(got, want) {
			t.Errorf("Words(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSubstringsAreNotWords(t *testing.T) {
	for _, name := range []string{"GlobalWeight", "Syntax", "Define", "Feedback", "Costume", "Refined", "Minority", "Discharged"} {
		if IsMoneyName(name) {
			t.Errorf("%s matched as money; testigo's substring tables read %q inside it", name, name)
		}
	}
	for _, name := range []string{"StandardFee", "Constant", "Instance", "Distance"} {
		if IsIdempotencyName(name) {
			t.Errorf("%s matched as an idempotency key: \"stan\" is a word in ISO 8583, not a substring", name)
		}
	}
	if IsCurrencyName("CurrentPage") || IsCurrencyName("Recurring") {
		t.Error("current and recurring are not currencies")
	}
}

func TestMoneyNamesStillMatch(t *testing.T) {
	for _, name := range []string{"Amount", "TotalAmount", "FeeCents", "amountMinor", "Balance", "Subtotal", "Fees", "RefundedAmount", "Price"} {
		if !IsMoneyName(name) {
			t.Errorf("%s should be read as money", name)
		}
	}
}

func TestACountIsNotAnAmount(t *testing.T) {
	for _, n := range []string{"TotalPages", "TotalCount", "PageSize", "RetryCount", "ItemCount", "TotalRecords"} {
		if IsMoneyName(n) {
			t.Errorf("%s counts things", n)
		}
	}
}

func TestARateIsNotAnAmountInAFloat(t *testing.T) {
	for _, n := range []string{"FeeRate", "TaxRate", "InterestRate", "DiscountPercent", "CommissionBps"} {
		if !IsMoneyName(n) {
			t.Errorf("%s is still money-related", n)
		}
		if IsMoneyAmountName(n) {
			t.Errorf("%s is a ratio; reporting it as money stored in a float sends a reviewer after the wrong thing", n)
		}
	}
	if !IsMoneyAmountName("FeeAmount") {
		t.Error("FeeAmount is an amount")
	}
}

func TestIdempotencyPhrasesNeedTheirWordsTogether(t *testing.T) {
	for _, n := range []string{"IdempotencyKey", "IdemKey", "RequestID", "request_id", "OrderID", "Rrn", "STAN", "TrackingNumber", "DedupKey"} {
		if !IsIdempotencyName(n) {
			t.Errorf("%s should be in the vocabulary", n)
		}
	}
	for _, n := range []string{"Request", "OrderStatus", "IDOrder", "RequestedAt"} {
		if IsIdempotencyName(n) {
			t.Errorf("%s is not a key name", n)
		}
	}
}

func TestStateNamesAreReadFromTheLastWord(t *testing.T) {
	for _, n := range []string{"PaymentStatus", "Status", "OrderState", "TxnStatuses", "Stage"} {
		if !IsStateType(n) {
			t.Errorf("%s should be a lifecycle name", n)
		}
	}
	for _, n := range []string{"StatusReport", "Thermostat", "RealEstate", "StatusCode"} {
		if IsStateType(n) {
			t.Errorf("%s is not a lifecycle name", n)
		}
	}
	if !IsWeakStateType("SettlementMode") || !IsWeakStateType("StatusCode") {
		t.Error("SettlementMode and StatusCode are weak names: an enum that may be a lifecycle")
	}
	if IsWeakStateType("PaymentStatus") {
		t.Error("a strong name is not weak")
	}
}

func TestPaginationStructsHoldNoMoney(t *testing.T) {
	for _, n := range []string{"Paging", "PaginationResponse", "PageMeta", "Cursor", "SearchFilter"} {
		if !IsContainerName(n) {
			t.Errorf("%s carries counters, not money", n)
		}
	}
	if IsContainerName("Order") {
		t.Error("Order is the entity that carries money")
	}
}

func TestMoneyTypesAreReadFromTheLastWords(t *testing.T) {
	for _, n := range []string{"Money", "PaymentAmount", "MinorUnits", "Decimal", "Balance"} {
		if !IsMoneyType(n) {
			t.Errorf("%s should be a money type", n)
		}
	}
	for _, n := range []string{"AmountFormatter", "BalanceSheetRow"} {
		if IsMoneyType(n) {
			t.Errorf("%s is not itself money", n)
		}
	}
}
