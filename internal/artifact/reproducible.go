package artifact

import (
	"os"
	"strconv"
	"time"
)

const reproducibleEpochDefault = 1577836800

func SourceDateEpoch() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Unix(reproducibleEpochDefault, 0).UTC()
}
