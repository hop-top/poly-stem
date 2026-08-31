package sessions

import (
	"errors"
	"strings"
)

// MinRefLength is the shortest accepted id prefix (spec §2.1).
const MinRefLength = 4

// Resolution errors. The CLI maps these to exit codes 2 / 3 / 4.
var (
	// ErrRefTooShort rejects prefixes under MinRefLength characters.
	ErrRefTooShort = errors.New("sessions: id prefix shorter than 4 characters")
	// ErrNotFound reports that no session matches the reference.
	ErrNotFound = errors.New("sessions: no session matches id")
	// ErrAmbiguous reports that the prefix matches several sessions.
	ErrAmbiguous = errors.New("sessions: ambiguous id prefix")
)

// Resolve addresses one session by full id or case-insensitive
// literal prefix (spec §2.1). An exact full-id match wins even when
// the reference is also a prefix of longer ids. On ErrAmbiguous the
// second return carries every candidate, newest first; it is nil for
// every other outcome.
func Resolve(recs []*Record, ref string) (*Record, []*Record, error) {
	if len(ref) < MinRefLength {
		return nil, nil, ErrRefTooShort
	}
	want := strings.ToLower(ref)

	var matches []*Record
	for _, r := range recs {
		id := strings.ToLower(r.Envelope.ID)
		if id == want {
			return r, nil, nil
		}
		if strings.HasPrefix(id, want) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil, ErrNotFound
	case 1:
		return matches[0], nil, nil
	}
	Sort(matches)
	return nil, matches, ErrAmbiguous
}
