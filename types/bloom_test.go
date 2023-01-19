package types

import "testing"

func TestBytesToBloom(t *testing.T) {
	bloom := BytesToBloom(nil)
	text, err := bloom.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	zeroBloomText, err := zeroBloom.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(zeroBloomText) == string(text) {
		t.Log("success: ", string(text))
	}
}
