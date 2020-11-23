package syscall

// ByteSliceFromString returns a NUL-terminated slice of bytes
// containing the text of s. If s contains a NUL byte at any
// location, it returns (nil, EINVAL).
func ByteSliceFromString(s string)([]byte,error){
	for i :=0;i< len(s);i++{
		if s[i] == 0{
			return nil,EINVAL
		}
	}
	a := make([]byte,len(s)+1)
	copy(a,s)
	return a,nil
}

// BytePtrFromString returns a pointer to a NUL-terminated array of
// bytes containing the text of s. If s contains a NUL byte at any
// location, it returns (nil, EINVAL).
func BytePtrFromString(s string) (*byte, error) {
	a,err :=ByteSliceFromString(s)
	if err != nil{
		return nil,err
	}
	return &a[0],nil
}
