package rawdb

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/ankr/dogesyncer/types"
)

func TestScanHash(t *testing.T) {
	ts := []string{
		"0xe9adff2de9482b6f4c1310ab6f0050b5fa9053096acb9b366b0a8a5b77edb9ff",
		"0x8a419becd8b5ec838f78d2927837a05de27a0b6a9a4cbbba1c7b06e95a536251",
	}
	var hs []types.Hash

	for _, v := range ts {
		hs = append(hs, types.StringToHash(v))
	}

	buf := &bytes.Buffer{}

	for _, v := range hs {
		buf.Write(v.Bytes())
	}

	scanner := bufio.NewScanner(buf)
	scanner.Split(ScanHash)
	for scanner.Scan() {
		t.Logf("hash: %s \n", types.BytesToHash(scanner.Bytes()))
	}
}
