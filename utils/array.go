package utils

func At[T any](s []T, i int) T {
	if i < 0 || i >= len(s) {
		var zero T
		return zero
	}
	return s[i]
}
