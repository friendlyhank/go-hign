package runtime

import "internal/bytealg"

// The Error interface identifies a run time error.
type Error interface {
	error

	// RuntimeError is a no-op function but
	// serves to distinguish types that are run time
	// errors from ordinary errors: a type is a
	// run time error if it has a RuntimeError method.
}

func panicwrap() {
	_ = bytealg.IndexByteString("", '(')
}
