package flagstack

import (
	"crypto/sha256"
	"encoding/binary"
)

func Bucket(environmentID, flagID, bucketValue string) int {
	input := "flagstack-v1\x00" + environmentID + "\x00" + flagID + "\x00" + bucketValue
	digest := sha256.Sum256([]byte(input))
	return int(binary.BigEndian.Uint32(digest[:4]) % BucketScale)
}
