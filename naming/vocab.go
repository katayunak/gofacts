package naming

var moneyField = NewVocab(
	"amount", "amt",
	"balance", "bal",
	"subtotal", "total",
	"price", "fee", "cost", "charge", "tax", "vat",
	"discount", "refund", "payout", "settlement",
	"credit", "debit",
	"money", "cash", "fund",
	"cents", "cent", "rial", "toman", "minor",
	"principal", "premium", "commission", "interest",
	"wage", "salary", "bonus", "penalty", "fine",
	"deposit", "withdraw", "withdrawal", "transfer",
)

var moneyType = NewVocab(
	"money", "amount", "currency", "decimal", "cents", "minor units", "minor unit",
	"price", "fee", "balance", "rial", "toman",
)

var currency = NewVocab("currency", "curr", "ccy", "iso4217", "currency code")

// counter words mark a number that counts things; TotalPages is not money even
// though it is a total.
var counter = NewVocab(
	"page", "pagination", "paging", "count", "counter",
	"index", "idx", "offset", "limit", "len", "length", "size", "num", "number",
	"row", "record", "item", "quantity", "qty", "capacity", "attempt", "retry",
	"second", "millis", "ms", "duration", "timeout", "ttl", "interval", "job", "lock",
)

// rate words mark a ratio. A fee rate in a float is a modelling choice worth
// a look, but it is not money stored in a float.
var rate = NewVocab(
	"rate", "ratio", "percent", "percentage", "pct", "bps", "basis point",
	"factor", "multiplier", "weight", "score", "probability",
)

var container = NewVocab(
	"paging", "pagination", "page", "meta", "metadata", "cursor", "filter",
	"query", "search", "config", "configuration", "setting", "option",
)

var idempotencyField = NewVocab(
	"idempotency", "idempotent", "idem",
	"request id", "reference id", "ref id", "refid",
	"correlation id", "trace id",
	"dedup", "deduplication",
	"client key", "client token",
	"unique key",
	"order id", "invoice id",
	"tracking number", "tracking no", "tracking code",
	"rrn", "retrieval reference", "stan",
	"transfer id", "payment id",
)

// A reconciler is named by the verb, or by the noun beside a job word: the
// noun alone also names its records and handlers (ReconciliationHandler,
// ParseReconFile).
var reconcileVerb = NewVocab("reconcile", "reconciling")

var reconcileNoun = NewVocab("recon", "reconciliation")

var jobWord = NewVocab("run", "job", "loop", "worker", "task", "sweep", "cron")

var reconcilerType = NewVocab("reconciler")

var runVerb = NewVocab("run", "start", "execute", "do", "process", "tick", "sweep")

// notAJob are the first words of the code around a reconciler that is not
// one: its constructor, options and accessors.
var notAJob = NewVocab(
	"new", "must", "make", "build", "provide", "with",
	"get", "list", "find", "load", "save", "set", "parse", "is", "has",
)

var stateStrong = NewVocab(
	"status", "state", "phase", "stage", "step", "lifecycle", "disposition",
	"transition", "progress", "stat", "sts", "situation", "position",
)

var stateWeak = NewVocab(
	"kind", "type", "code", "result", "outcome", "mode", "reason", "category",
	"class", "level", "flag", "action", "event", "operation", "direction",
)

var finalState = NewVocab(
	"completed", "complete", "success", "succeeded", "settled", "captured",
	"finished", "done", "closed", "failed", "failure", "cancelled", "canceled",
	"rejected", "declined", "expired", "voided", "refunded", "reversed",
	"abandoned", "terminated", "archived",
)

var failureState = NewVocab(
	"failed", "failure", "declined", "rejected", "error", "errored", "cancelled",
	"canceled", "expired", "voided", "aborted",
)

var reopeningState = NewVocab(
	"refund", "chargeback", "dispute", "reversal", "reverse", "adjust",
	"adjustment", "correction", "compensate", "compensation", "retry", "reopen", "recall",
)

// IsMoneyName: a field, parameter or variable named like an amount and not
// like a counter.
func IsMoneyName(name string) bool { return moneyField.Matches(name) && !counter.Matches(name) }

// IsMoneyAmountName additionally excludes ratios, for the claim "money is
// stored in a float".
func IsMoneyAmountName(name string) bool { return IsMoneyName(name) && !rate.Matches(name) }

func IsMoneyType(name string) bool       { return moneyType.MatchesLast(name) }
func IsCurrencyName(name string) bool    { return currency.Matches(name) }
func IsCounterName(name string) bool     { return counter.Matches(name) }
func IsRateName(name string) bool        { return rate.Matches(name) }
func IsContainerName(name string) bool   { return container.Matches(name) }
func IsIdempotencyName(name string) bool { return idempotencyField.Matches(name) }

// ReconcilerName returns the words that name a function as a repair job: the
// verb (Reconcile, ReconcilingLoop), or recon or reconciliation with a job
// word (RunSettlementRecon). It returns nothing when the first word makes the
// function a constructor, option or accessor (NewReconciler, WithReconciler,
// GetReconciliation).
func ReconcilerName(name string) []string {
	words := Words(name)
	if len(words) == 0 || notAJob.starts(words) {
		return nil
	}
	if verb, ok := reconcileVerb.find(words); ok {
		return []string{verb}
	}
	noun, isNoun := reconcileNoun.find(words)
	job, isJob := jobWord.find(words)
	if isNoun && isJob {
		return []string{noun, job}
	}
	return nil
}

func IsReconcilerName(name string) bool { return len(ReconcilerName(name)) > 0 }

// IsReconcilerType: a type named like a repair job (SettlementReconciler,
// ReconJob), whose run method is the job.
func IsReconcilerType(name string) bool {
	words := Words(name)
	if reconcilerType.ends(words) {
		return true
	}
	_, isNoun := reconcileNoun.find(words)
	_, isVerb := reconcileVerb.find(words)
	return (isNoun || isVerb) && jobWord.ends(words)
}

// IsRunMethod: a method name that starts with a run verb (Run, Start,
// ProcessBatch).
func IsRunMethod(name string) bool { return runVerb.starts(Words(name)) }

// IsStateType: named like a lifecycle (PaymentStatus).
func IsStateType(name string) bool { return stateStrong.MatchesLast(name) }

// IsWeakStateType: named like an enum that may or may not be a lifecycle
// (SettlementMode). Promotion needs behaviour, not more name.
func IsWeakStateType(name string) bool {
	return !stateStrong.MatchesLast(name) && stateWeak.MatchesLast(name)
}

func IsFinalState(name string) bool     { return finalState.Matches(name) }
func IsFailureState(name string) bool   { return failureState.Matches(name) }
func IsReopeningState(name string) bool { return reopeningState.Matches(name) }
