package secrets

import (
	"encoding/binary"
	"testing"
)

func TestSixDigitDerivationRejectsOutOfRangeCandidatesBeforeModulo(t *testing.T) {
	var counters []uint64
	code := deriveSixDigitCode(func(counter uint64) [32]byte {
		counters = append(counters, counter)
		var block [32]byte
		if counter == 0 {
			// Every uint32 in the first block is above the acceptance limit.
			// A biased direct-modulo implementation would incorrectly use one.
			for offset := 0; offset < len(block); offset += 4 {
				binary.BigEndian.PutUint32(block[offset:offset+4], ^uint32(0))
			}
			return block
		}
		binary.BigEndian.PutUint32(block[:4], 42)
		return block
	})

	if code != "000042" {
		t.Fatalf("derived code = %q, want the accepted second-block value 000042", code)
	}
	if len(counters) != 2 || counters[0] != 0 || counters[1] != 1 {
		t.Fatalf("derived counters = %v, want [0 1]", counters)
	}
}
