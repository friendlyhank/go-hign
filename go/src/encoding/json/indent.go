package json

import "bytes"

// compact -
func compact(dst *bytes.Buffer, src []byte, escape bool) error {
	start := 0
	//处理特殊符号
	for i,c := range src{
		if escape && (c == '<' || c == '>' || c == '&') {
			if start < i {
				dst.Write(src[start:i])
				start =i +1
			}
		}
	}
	if start < len(src) {
		dst.Write(src[start:])
	}
	return nil
}
