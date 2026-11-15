//go:build linux

package xattr_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alexfalkowski/go-linux/xattr"
)

func ExampleSet() {
	opened, err := os.CreateTemp("", "xattr-example-")
	if err != nil {
		panic(err)
	}
	defer os.Remove(opened.Name())
	defer opened.Close()

	if err := xattr.Set(opened, "user.content_type", []byte("text/plain"), xattr.Create); err != nil {
		panic(err)
	}
	value, err := xattr.Get(opened, "user.content_type")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(value))
	if err := xattr.Remove(opened, "user.content_type"); err != nil {
		panic(err)
	}
	// Output: text/plain
}

func ExampleGetAt() {
	root, err := os.MkdirTemp("", "xattr-at-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	if err := os.WriteFile(filepath.Join(root, "data"), nil, 0o600); err != nil {
		panic(err)
	}
	dir, err := os.Open(root)
	if err != nil {
		panic(err)
	}
	defer dir.Close()

	options := xattr.PathOptions{NoFollow: true}
	if err := xattr.SetAt(dir, "data", "user.status", []byte("ready"), xattr.Upsert, options); err != nil {
		panic(err)
	}
	value, err := xattr.GetAt(dir, "data", "user.status", options)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(value))
	// Output: ready
}
