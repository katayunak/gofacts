package fact

// Seam is a call that crosses the process boundary. Injectable means a test
// can make the call fail: it goes through an interface the module defines, or
// it is made on a *http.Client the test can hand in, whose Transport (Iface
// net/http.RoundTripper) is the fake. That part is proved by SSA. Kind comes
// from package tables and is a heuristic.
type Seam struct {
	In         CodeRef  `json:"in"`
	Kind       SeamKind `json:"kind"`
	Target     string   `json:"target"`
	Line       int      `json:"line"`
	Injectable bool     `json:"injectable"`
	Iface      string   `json:"iface,omitempty"`

	// HandedIn is set for a call on a concrete client that is still
	// injectable: the parameter or field through which a test hands in its
	// own, as "parameter c" or "field example.com/paysvc/psp.Client.http".
	// It names where the client comes from, or where its Transport comes from
	// when the function builds the client itself.
	HandedIn string `json:"handed_in,omitempty"`
}

func (s Seam) At() Pos { return Pos{File: s.In.File, Line: s.Line} }

type SeamKind string

const (
	SeamDB     SeamKind = "db"
	SeamHTTP   SeamKind = "http"
	SeamQueue  SeamKind = "queue"
	SeamCache  SeamKind = "cache"
	SeamClock  SeamKind = "clock"
	SeamRandom SeamKind = "random"
)

// Network reports whether the kind is a round trip to another process that a
// database transaction should not wait on.
func (k SeamKind) Network() bool { return k == SeamHTTP || k == SeamQueue }
