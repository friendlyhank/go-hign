package runtime

import "testing"

func eqstring_generic(s1, s2 string) bool {
	if len(s1) != len(s2){
		return false
	}
	// optimization in assembly versions:
	// if s1.str == s2.str { return true }
	for i :=0;i < len(s1);i++{
		if s1[i] != s2[i]{
			return false
		}
	}
	return true
}

func TestEqString(t *testing.T) {
	// This isn't really an exhaustive test of == on strings, it's
	// just a convenient way of documenting (via eqstring_generic)
	// what == does.
	s := []string{
		"",
		"a",
		"c",
		"aaa",
		"ccc",
		"cccc"[:3], // same contents, different string
		"1234567890",
	}
	for _, s1 := range s {
		for _,s2 := range s{
			x := s1 == s2
			y := eqstring_generic(s1, s2)
			if x != y {
				println("inin")
			}
		}
	}
}