package helpers

import "strconv"

func IntPtrToString(v *int) string {
	if v == nil {
		return "null"
	}
	return strconv.Itoa(*v)
}

func IntToString(v int) string {
	return strconv.Itoa(v)
}
