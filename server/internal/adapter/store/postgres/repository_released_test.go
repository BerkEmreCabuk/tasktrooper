package postgres

import "testing"

// The archive search is what someone uses to find work that has left the board,
// and the thing they remember is usually the key. "T-12", "t-12" and "12" must
// all reach task 12, and anything that is not a number must match no row rather
// than every row.
func TestTaskNumberFromQuery(t *testing.T) {
	for query, want := range map[string]string{
		"T-12":  "12",
		"t-12":  "12",
		"DE-7":  "7",
		"12":    "12",
		"login": "-1",
		"T-":    "-1",
		"":      "-1",
		"T-1a":  "-1",
	} {
		if got := taskNumberFromQuery(query); got != want {
			t.Errorf("taskNumberFromQuery(%q) = %q, want %q", query, got, want)
		}
	}
}
