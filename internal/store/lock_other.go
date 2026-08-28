//go:build windows

package store

// lock is unavailable on Windows until the Room store has a real cross-process
// implementation. Concurrent Windows use is unsupported.
func (f *File) lock() (func(), error) {
	return func() {}, nil
}
