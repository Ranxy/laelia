//nolint:revive
package utils

func Map[T any, R any, TS ~[]T, RS ~[]R](input TS, f func(T) R) RS {
	result := make(RS, len(input))
	for i, v := range input {
		result[i] = f(v)
	}
	return result
}
