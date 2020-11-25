package runtime

import "internal/bytealg"

func panicwrap() {
	_ = bytealg.IndexByteString("", '(')
}
