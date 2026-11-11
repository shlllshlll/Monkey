package util

type Stack[T any] []T

func (s *Stack[T]) Push(v T) {
	*s = append(*s, v)
}

func (s *Stack[T]) Pop() (T, bool) {
	if len(*s) == 0 {
		var zero T
		return zero, false
	}

	idx := len(*s) - 1
	v := (*s)[idx]
	*s = (*s)[:idx]

	return v, true
}

func (s *Stack[T]) Peek() (T, bool) {
	if len(*s) == 0 {
		var zero T
		return zero, false
	}

	idx := len(*s) - 1
	v := (*s)[idx]

	return v, true
}

func (s *Stack[T]) Len() int {
	return len(*s)
}
