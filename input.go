package gonfig

import (
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Input stores information about given struct
type Input struct {
	// Struct name is used for error messages
	Name string

	// Fields information
	Fields []*Field
}

// Struct field information
type Field struct {
	// Field value
	Value reflect.Value

	// Field tags
	Tags *ConfigTags

	// Slice of field names from root of struct all the way down to the field
	Path []string

	// IsSet specifies whether field value is set by one of the providers
	IsSet bool
}

// NewInput validates and returns a new Input with all settable fields
// Input argument must be a non-nil struct pointer
func NewInput(i interface{}) (*Input, error) {
	v := reflect.ValueOf(i)

	if err := checkInput(v); err != nil {
		return nil, err
	}

	in := Input{
		Name: v.Type().String(),
	}

	f := Field{
		Value: v.Elem(),
		Tags:  &ConfigTags{},
	}

	if err := in.traverseFiled(&f); err != nil {
		return nil, err
	}

	return &in, nil
}

// checkInput checks for a non-nil struct pointer
func checkInput(v reflect.Value) error {
	if v.Type() == nil ||
		v.Type().Kind() != reflect.Ptr ||
		v.IsNil() ||
		v.Type().Elem().Kind() != reflect.Struct {
		return &InvalidInputError{
			Type:  v.Type(),
			Value: v,
		}
	}

	return nil
}

// traverseFiled recursively traverse all fields and collect their information
func (in *Input) traverseFiled(f *Field) error {
	if !f.Value.CanSet() || f.Tags.Ignore {
		return nil
	}

	switch f.Value.Kind() {
	case reflect.Struct:
		if isTime(f.Value) || isURL(f.Value) {
			in.collectField(f)

			return nil
		}

		for i := 0; i < f.Value.NumField(); i++ {
			nestedField := Field{
				Value: f.Value.Field(i),
				Tags:  getTags(f.Value.Type().Field(i).Tag),
				Path:  append(f.Path, f.Value.Type().Field(i).Name),
			}

			if err := in.traverseFiled(&nestedField); err != nil {
				return err
			}
		}

	case reflect.Ptr:
		pv := reflect.New(f.Value.Type().Elem())
		f.Value.Set(pv)

		pointedField := Field{
			Value: pv.Elem(),
			Tags:  f.Tags,
			Path:  f.Path,
		}

		return in.traverseFiled(&pointedField)

	case reflect.Slice, reflect.Array:
		switch f.Value.Type().Elem().Kind() {
		case reflect.Slice,
			reflect.Array,
			reflect.Uintptr,
			reflect.Chan,
			reflect.Func,
			reflect.Interface,
			reflect.UnsafePointer:
			return fmt.Errorf(
				unsupportedElementTypeErrFormat,
				ErrUnsupportedType, f.Value.Type().Elem().Kind(), in.getPath(f.Path),
			)

		default:
			in.collectField(f)
		}

	case reflect.Uintptr,
		reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.UnsafePointer:
		return fmt.Errorf(
			unsupportedTypeErrFormat,
			ErrUnsupportedType, f.Value.Kind(), in.getPath(f.Path),
		)

	default:
		in.collectField(f)
	}

	return nil
}

func (in *Input) collectField(f *Field) {
	in.Fields = append(in.Fields, f)
}

// setValue validates and sets the value of a struct field
func (in *Input) setValue(f *Field, value string) error {
	if f.Tags.Expand {
		value = os.ExpandEnv(value)
	}

	switch f.Value.Kind() {
	case reflect.String:
		f.Value.SetString(value)

	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf(
				parseErrFormat,
				ErrParsing, in.getPath(f.Path), err,
			)
		}

		f.Value.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var d time.Duration
		var i int64
		var err error

		if isDuration(f.Value) {
			d, err = time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf(
					parseErrFormat,
					ErrParsing, in.getPath(f.Path), err,
				)
			}

			i = int64(d)
		} else {
			i, err = strconv.ParseInt(value, 0, 64)
			if err != nil {
				return fmt.Errorf(
					parseErrFormat,
					ErrParsing, in.getPath(f.Path), err,
				)
			}
		}

		if f.Value.OverflowInt(i) {
			return fmt.Errorf(
				overflowErrFormat,
				ErrValueOverflow, i, f.Value.Kind(), in.getPath(f.Path),
			)
		}

		f.Value.SetInt(i)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := strconv.ParseUint(value, 0, 64)
		if err != nil {
			return fmt.Errorf(
				parseErrFormat,
				ErrParsing, in.getPath(f.Path), err,
			)
		}

		if f.Value.OverflowUint(i) {
			return fmt.Errorf(
				overflowErrFormat,
				ErrValueOverflow, i, f.Value.Kind(), in.getPath(f.Path),
			)
		}

		f.Value.SetUint(i)

	case reflect.Float32, reflect.Float64:
		fv, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf(
				parseErrFormat,
				ErrParsing, in.getPath(f.Path), err,
			)
		}

		if f.Value.OverflowFloat(fv) {
			return fmt.Errorf(
				overflowErrFormat,
				ErrValueOverflow, fv, f.Value.Kind(), in.getPath(f.Path),
			)
		}

		f.Value.SetFloat(fv)

	case reflect.Complex64, reflect.Complex128:
		cx, err := strconv.ParseComplex(value, 64)
		if err != nil {
			return fmt.Errorf(
				parseErrFormat,
				ErrParsing, in.getPath(f.Path), err,
			)
		}

		if f.Value.OverflowComplex(cx) {
			return fmt.Errorf(
				overflowErrFormat,
				ErrValueOverflow, cx, f.Value.Kind(), in.getPath(f.Path),
			)
		}

		f.Value.SetComplex(cx)

	case reflect.Slice, reflect.Array:
		switch f.Value.Type().Elem().Kind() {
		case reflect.Slice,
			reflect.Array,
			reflect.Uintptr,
			reflect.Chan,
			reflect.Func,
			reflect.Interface,
			reflect.UnsafePointer:
			return fmt.Errorf(
				unsupportedElementTypeErrFormat,
				ErrUnsupportedType, f.Value.Type().Elem().Kind(), in.getPath(f.Path),
			)
		}

		var items []string
		for _, v := range strings.Split(value, f.Tags.Separator) {
			item := strings.TrimSpace(v)
			if len(item) > 0 {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			return nil
		}

		switch f.Value.Kind() {
		case reflect.Slice:
			size := len(items)
			sv := reflect.MakeSlice(reflect.SliceOf(f.Value.Type().Elem()), size, size)

			for i := range items {
				nestedField := Field{
					Value: sv.Index(i),
					Tags:  f.Tags,
					Path:  f.Path,
				}

				if err := in.setValue(&nestedField, items[i]); err != nil {
					return err
				}
			}

			f.Value.Set(sv)

		case reflect.Array:
			size := f.Value.Len()
			if size == 0 {
				return nil
			}

			at := reflect.ArrayOf(size, f.Value.Type().Elem())
			av := reflect.New(at).Elem()

			for i := 0; i < size; i++ {
				nestedField := Field{
					Value: av.Index(i),
					Tags:  f.Tags,
					Path:  f.Path,
				}

				if err := in.setValue(&nestedField, items[i]); err != nil {
					return err
				}
			}

			f.Value.Set(av)
		}

	case reflect.Map:
		// TODO

	case reflect.Ptr:
		pv := reflect.New(f.Value.Type().Elem())
		f.Value.Set(pv)
		pointedField := Field{
			Value: pv.Elem(),
			Tags:  f.Tags,
			Path:  f.Path,
		}

		return in.setValue(&pointedField, value)

	case reflect.Struct:
		if isTime(f.Value) {
			t, err := time.Parse(f.Tags.Format, value)
			if err != nil {
				return fmt.Errorf(
					parseErrFormat,
					ErrParsing, in.getPath(f.Path), err,
				)
			}

			f.Value.Set(reflect.ValueOf(t))
			return nil
		}

		if isURL(f.Value) {
			u, err := url.Parse(value)
			if err != nil {
				return fmt.Errorf(
					parseErrFormat,
					ErrParsing, in.getPath(f.Path), err,
				)
			}

			f.Value.Set(reflect.ValueOf(*u))
			return nil
		}

	default:
		return fmt.Errorf(
			unsupportedTypeErrFormat,
			ErrUnsupportedType, f.Value.Kind(), in.getPath(f.Path),
		)
	}

	return nil
}

// getPath returns a dot separated string prefixed with struct name
func (in *Input) getPath(paths []string) string {
	return in.Name + "." + strings.Join(paths, ".")
}