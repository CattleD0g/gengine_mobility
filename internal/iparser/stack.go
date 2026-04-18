package iparser

// Stack is a minimal LIFO stack of arbitrary values used by the parser listener
// while building the AST. It replaces github.com/golang-collections/collections/stack.
type Stack struct {
	items []interface{}
}

func NewStack() *Stack {
	return &Stack{}
}

func (s *Stack) Push(v interface{}) {
	s.items = append(s.items, v)
}

func (s *Stack) Pop() interface{} {
	n := len(s.items)
	if n == 0 {
		return nil
	}
	v := s.items[n-1]
	s.items[n-1] = nil
	s.items = s.items[:n-1]
	return v
}

func (s *Stack) Peek() interface{} {
	n := len(s.items)
	if n == 0 {
		return nil
	}
	return s.items[n-1]
}

func (s *Stack) Len() int {
	return len(s.items)
}
