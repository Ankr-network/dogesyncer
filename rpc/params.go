package rpc

import (
	"errors"
	"reflect"
)

func GetPrams(params ...any) ([]any, error) {
	res := []any{}
	if reflect.Slice == reflect.TypeOf(params).Kind() {
		if reflect.Slice == reflect.TypeOf(params[0]).Kind() {
			s := reflect.ValueOf(params[0])
			for i := 0; i < s.Len(); i++ {
				res = append(res, s.Index(i).Interface())
			}
			return res, nil
		}
		return nil, errors.New("Invalid params")
	} else {
		return nil, errors.New("Invalid params")
	}
}
