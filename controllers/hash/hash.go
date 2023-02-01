// package hash...
// https://github.com/kubernetes/kubernetes/blob/master/pkg/util/hash/hash.go
// https://github.com/elastic/cloud-on-k8s/blob/main/pkg/controller/common/hash/hash.go
package hash

import (
	"fmt"
	"hash"
	"hash/fnv"

	"github.com/davecgh/go-spew/spew"
)

// deepHashObject writes specified object to hash using the spew library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
func deepHashObject(hasher hash.Hash, objectToWrite interface{}) {
	hasher.Reset()

	printer := spew.ConfigState{
		Indent:         " ",
		SortKeys:       true,
		DisableMethods: true,
		SpewKeys:       true,
	}

	printer.Fprintf(hasher, "%#v", objectToWrite)
}

// Object returns a hash of a given object using the 32-bit FNV-1 hash function
// and the spew library to print the object (see deepHashObject).
func Object(object interface{}) string { //nolint:revive
	objHash := fnv.New32a()
	deepHashObject(objHash, object)

	return fmt.Sprint(objHash.Sum32())
}
