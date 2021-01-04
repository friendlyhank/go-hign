package strings

// Index returns the index of the first instance of substr in s, or -1 if substr is not present in s.
//返回字符串substr在字符串s中第一次出现的位置,-1表示
func Index(s,substr string)int{
	n :=len(substr)
	switch {
	case n == 0:
		return 0
	case n == 1:
	case n == len(s):
		if substr == s{
			return 0
		}
		return -1
	case n > len(s):
		return -1
	}
	return -1
}
