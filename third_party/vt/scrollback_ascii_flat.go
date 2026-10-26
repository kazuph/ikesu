//go:build flatcells

package vt

// pushPlainScreenBytes is only called for a verified, unstyled ASCII screen row.
// Its original column count still determines the unchanged history byte charge.
func (s *Scrollback) pushPlainScreenBytes(text []byte, columns int) bool {
	if columns != len(text) {
		panic("flat ASCII screen row: inconsistent column count")
	}
	return s.pushByteRow(nil, columns, true, text)
}
