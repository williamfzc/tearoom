//go:build windows || js

package store

// lock is a no-op on platforms without flock.
//
// On js/wasm this is correct: the browser build never touches a filesystem store,
// it talks to the server over HTTP, and the server does the locking. On Windows it
// is a known gap rather than a design choice — cross-process safety there needs
// LockFileEx, and shipping a silent no-op would be worse than not shipping, so the
// CLI refuses to run a file store on Windows until that lands.
func (f *File) lock() (func(), error) {
	return func() {}, nil
}
