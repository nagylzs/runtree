package events

import (
	"sync"
	"sync/atomic"
)

/*
Debouncer can be used to debounce calls to a function with a single parameter of T.

You create it with NewDebouncer. Unlike regular "delayed" debouncers, this one focuses on rapid response, as follows.

  - For each T value, a separate call cycle can be started with Call(value)

  - Each call cycle is a loop that calls the underlying function with the given value,
    and it keeps calling it until no more Call request comes in (while the loop is running)

It is guaranteed that the underlying function will not be called multiple times in parallel, but the number of actual
calls is somewhat indeterministic, because there is a race condition between Call and call cycles.
*/
type Debouncer[T comparable] struct {
	chs   map[T]*atomic.Bool
	lck   sync.Mutex
	call  func(T)
	cache bool
}

// NewDebouncer creates a new Debouncer instance.
//
// The `cache` parameter determines whether the debouncer should clean up its internal cache after
// the call cycle has finished. If you need to use it with a fixed/small number of different
// values, then using cache=true will utilize an internal cache better, and make it more
// efficient. If you need to use it with a large or unpredictable number of values,
// then using cache=false will prevent allocating too many resources.
//
// Parameters:
// - call: The underlying function that needs to be debounced.
// - cache: A boolean indicating whether to clean up the cache after each call.
//
// Returns:
// - *Debouncer[T]: A pointer to the newly created Debouncer instance.
func NewDebouncer[T comparable](call func(T), cache bool) *Debouncer[T] {
	return &Debouncer[T]{
		chs:   make(map[T]*atomic.Bool),
		lck:   sync.Mutex{},
		call:  call,
		cache: cache,
	}
}

func (db *Debouncer[T]) Call(value T) {
	db.lck.Lock()
	defer db.lck.Unlock()

	ch, ok := db.chs[value]
	if !ok {
		ch = &atomic.Bool{}
		db.chs[value] = ch
	}
	wasRunning := ch.Load()
	ch.Store(true)
	// If it was not running yet, then we will be responsible for making the call(s).
	if !wasRunning {
		go db.callUntilDone(value)
	}
}

func (db *Debouncer[T]) callUntilDone(value T) {
	need := true
	for need {
		// do the call
		db.call(value)
		need = false
		// let's see if anybody else wanted to make a call in the meantime
		db.lck.Lock()
		ch, ok := db.chs[value]
		if ok {
			need = ch.Load()
			ch.Store(false)
		}
		db.lck.Unlock()
	}
	if !db.cache {
		db.clean(value)
	}
}

// clean: remove entry from the map, if it is unused
func (db *Debouncer[T]) clean(value T) {
	db.lck.Lock()
	defer db.lck.Unlock()
	ch, ok := db.chs[value]
	if !ok {
		return
	}
	if !ch.Load() {
		delete(db.chs, value)
	}
}
