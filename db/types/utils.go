package types

import (
	"encoding/binary"
	"fmt"
	"strconv"
)

const Splitter = '/'

func FromUint64(v uint64) []byte {
	return []byte(fmt.Sprintf("%d", v))
}

func ToUint64(v []byte) (uint64, error) {
	data, err := strconv.ParseUint(string(v), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse uint64 from %s: %w", string(v), err)
	}

	return data, nil
}

func FromUint64Key(v uint64) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, v)
	return data
}

func ToUint64Key(data []byte) (v uint64) {
	return binary.BigEndian.Uint64(data)
}
