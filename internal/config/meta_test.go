package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lucasassuncao/yedit/spec"
)

// yamlNames lists the yaml keys a struct's fields are tagged with.
func yamlNames(t reflect.Type) map[string]bool {
	names := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		names[name] = true
	}
	return names
}

// A key yedit does not know is dropped on decode, so the constraint it
// carries would quietly stop being enforced.
func TestMetaKeysAreFieldMetaKeys(t *testing.T) {
	known := yamlNames(reflect.TypeFor[spec.FieldMeta]())
	for name := range yamlNames(reflect.TypeFor[meta]()) {
		if !known[name] {
			t.Errorf("meta declares %q, which spec.FieldMeta has no field for", name)
		}
	}
}
