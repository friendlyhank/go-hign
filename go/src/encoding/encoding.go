package encoding

// TextMarshaler is the interface implemented by an object that can
// marshal itself into a textual form.
//
// MarshalText encodes the receiver into UTF-8-encoded text and returns the result.
type TextMarshaler interface {
	MarshalText() (text []byte, err error)
}

// TextUnmarshaler is the interface implemented by an object that can
// unmarshal a textual representation of itself.
//
// UnmarshalText must be able to decode the form generated by MarshalText.
// UnmarshalText must copy the text if it wishes to retain the text
// after returning.
type TextUnmarshaler interface {
	UnmarshalText(text []byte) error
}
