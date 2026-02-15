package sets

type Set[T comparable] map[T]struct{}

func New[T comparable](items ...T) Set[T] {
	set := make(Set[T], len(items))
	set.Insert(items...)
	return set
}

func (s Set[T]) Insert(items ...T) {
	for _, item := range items {
		s[item] = struct{}{}
	}
}

func (s Set[T]) Has(item T) bool {
	_, ok := s[item]
	return ok
}

func (s Set[T]) Union(other Set[T]) Set[T] {
	result := New[T]()

	for item := range s {
		result.Insert(item)
	}

	for item := range other {
		result.Insert(item)
	}

	return result
}

func (s Set[T]) DestructiveUnion(other Set[T]) {
	for item := range other {
		s.Insert(item)
	}
}

func (s Set[T]) Len() int {
	return len(s)
}
