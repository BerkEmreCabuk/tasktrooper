package domain

import "encoding/json"

// Nullable is a request field that can tell "the client did not mention this"
// from "the client explicitly cleared it". A plain *T cannot: encoding/json
// decodes an omitted key and an explicit `null` to the same nil pointer. This
// exists because the board's "unassign this agent" control sends
// {"assignee_agent_id": null} and the update must clear, not skip, the field.
// It is deliberately NOT used for every optional field — text fields are cleared
// by sending "", which already works.
type Nullable[T any] struct {
	// Present is whether the key appeared in the request body at all.
	Present bool
	// Value is nil when the key appeared as `null`.
	Value *T
}

func (n *Nullable[T]) UnmarshalJSON(b []byte) error {
	n.Present = true
	if string(b) == "null" {
		n.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	n.Value = &v
	return nil
}

func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*n.Value)
}

// SetNullable builds a field that is present and carries a value — for callers
// that construct a request in Go, where "present" is never in doubt.
func SetNullable[T any](v T) Nullable[T] { return Nullable[T]{Present: true, Value: &v} }

// ClearNullable builds a field that is present and empty — what `null` on the
// wire decodes to.
func ClearNullable[T any]() Nullable[T] { return Nullable[T]{Present: true} }
