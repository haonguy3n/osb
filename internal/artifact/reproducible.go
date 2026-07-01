package artifact

import (
	"os"
	"strconv"
	"time"
)

// reproducibleEpochDefault is the timestamp osb stamps into artifacts when
// SOURCE_DATE_EPOCH is unset: 2020-01-01T00:00:00Z. A fixed reference keeps two
// builds of the same inputs byte-identical.
const reproducibleEpochDefault = 1577836800

// SourceDateEpoch returns the timestamp osb writes into artifact metadata (tar
// mtimes, package build dates) so builds are reproducible. It honors the
// SOURCE_DATE_EPOCH environment variable — seconds since the Unix epoch, the
// reproducible-builds standard — and falls back to a fixed reference time so
// identical inputs still yield identical bytes.
func SourceDateEpoch() time.Time {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Unix(reproducibleEpochDefault, 0).UTC()
}
