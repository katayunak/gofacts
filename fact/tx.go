package fact

// Tx is one database transaction as the code opens it: where it begins, every
// place it can end, and what happens while it is open. The window is computed
// on the SSA control-flow graph, so a call after Commit is not "inside" it.
type Tx struct {
	In    CodeRef `json:"in"`
	Begin Pos     `json:"begin"`
	Via   string  `json:"via"`

	// Wrapper is set when a library helper owns begin, commit and rollback
	// (RunInTransaction, gorm Transaction, pgx.BeginFunc) and the module only
	// supplies the callback.
	Wrapper bool `json:"wrapper,omitempty"`

	// OpenedAt is set when Begin is a call to a module function that begins
	// the transaction and returns it: OpenedAt is the begin inside that
	// function, and the window is judged here, where the transaction ends.
	OpenedAt *Pos `json:"opened_at,omitempty"`

	Commits          []Pos `json:"commits,omitempty"`
	Rollbacks        []Pos `json:"rollbacks,omitempty"`
	DeferredRollback bool  `json:"deferred_rollback,omitempty"`

	// Escapes means the transaction value is returned, stored or handed to
	// another function, so its end is decided somewhere else. Leaks are not
	// reported for an escaping transaction. When a module function only returns
	// it, each static call to that function is its own Tx, with OpenedAt set.
	Escapes bool `json:"escapes,omitempty"`

	// Leaks are returns reachable from the begin with neither a commit, a
	// rollback nor a deferred rollback on the way.
	Leaks []Pos `json:"leaks,omitempty"`

	// Used is false when no statement runs on the transaction at all, so
	// everything between begin and commit happens outside it.
	Used bool `json:"used"`

	Inside []TxEffect `json:"inside,omitempty"`
}

// TxEffect is a boundary crossing that happens while the transaction is open.
// Via is the module function it happens through when it is not direct.
type TxEffect struct {
	At       Pos      `json:"at"`
	Kind     SeamKind `json:"kind"`
	Target   string   `json:"target"`
	Via      string   `json:"via,omitempty"`
	OnTx     bool     `json:"on_tx,omitempty"`
	Bypasses bool     `json:"bypasses,omitempty"`
	Basis    Basis    `json:"basis,omitempty"`
}
