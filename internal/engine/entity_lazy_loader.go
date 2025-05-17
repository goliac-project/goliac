package engine

// generic lazy loader entity
// it will be used for the Reconciliator to load the entity from the local or remote
type LazyLoaderEntity interface {
	string | *GithubEnvironment
}

type MappedEntityLazyLoader[T LazyLoaderEntity] interface {
	GetEntity() map[string]T
}

// the local version of the lazy loader
// is just a wrapper around a map
type LocalLazyLoader[T LazyLoaderEntity] struct {
	entity map[string]T
}

func NewLocalLazyLoader[T LazyLoaderEntity](entity map[string]T) *LocalLazyLoader[T] {
	return &LocalLazyLoader[T]{entity: entity}
}

func (l *LocalLazyLoader[T]) GetEntity() map[string]T {
	return l.entity
}

// the remote version of the lazy loader
// contains a deferred function to load the entity
type RemoteLazyLoader[T LazyLoaderEntity] struct {
	load   func() map[string]T
	entity map[string]T
}

func NewRemoteLazyLoader[T LazyLoaderEntity](load func() map[string]T) *RemoteLazyLoader[T] {
	return &RemoteLazyLoader[T]{load: load}
}

func (l *RemoteLazyLoader[T]) GetEntity() map[string]T {
	if l.entity == nil {
		l.entity = l.load()
	}
	return l.entity
}

// func (l *RemoveMutableLazyLoader[T]) Remove(key string) {
// 	delete(l.entity, key)
// }

// func (l *RemoveMutableLazyLoader[T]) Add(key string, value T) {
// 	l.entity[key] = value
// }

// func (l *RemoveMutableLazyLoader[T]) Update(key string, value T) {
// 	l.entity[key] = value
// }
