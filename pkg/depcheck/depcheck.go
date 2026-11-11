// Package depcheck validates the dependency struct a constructor receives and names what is missing.
package depcheck

import (
	"fmt"
	"reflect"
	"strings"
)

// validator is implemented by dependency values that know whether they are usable.
type validator interface {
	Valid() bool
}

// Required returns an error naming every exported field of deps (a struct or a pointer to one) that is missing:
// nil pointers, interfaces, maps, slices, functions and channels; empty strings; numbers that are not positive;
// zero structs, or structs whose Valid method says no. Bools and fields tagged `depcheck:"optional"` are skipped.
func Required(prefix string, deps any) error {
	v := reflect.ValueOf(deps)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return fmt.Errorf("%s: deps are required", prefix)
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("%s: deps must be a struct, got %T", prefix, deps)
	}
	var missing []string
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() || f.Tag.Get("depcheck") == "optional" {
			continue
		}
		if name, ok := check(f.Name, v.Field(i)); !ok {
			missing = append(missing, name)
		}
	}
	switch len(missing) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("%s: %s is required", prefix, missing[0])
	default:
		return fmt.Errorf("%s: %s are required", prefix, strings.Join(missing, ", "))
	}
}

// check reports whether the field holds a usable value, and how to name it when it does not.
func check(name string, v reflect.Value) (string, bool) {
	switch v.Kind() {
	case reflect.Bool:
		return name, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "positive " + name, v.Int() > 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return "positive " + name, v.Uint() > 0
	case reflect.Float32, reflect.Float64:
		return "positive " + name, v.Float() > 0
	case reflect.String:
		return name, v.String() != ""
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return name, !v.IsNil()
	case reflect.Struct:
		if val, ok := v.Interface().(validator); ok {
			return "valid " + name, val.Valid()
		}
		return name, !v.IsZero()
	default:
		return name, !v.IsZero()
	}
}
