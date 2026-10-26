package vt

// ScreenStorageInfo describes retained cell storage, not the whole process heap.
// IDArrayBytes excludes map buckets and string/color payloads; runtime GC metrics
// measure the complete heap separately rather than treating this as its total.
type ScreenStorageInfo struct {
	CellBytes, CellCapacity, IDArrayBytes, LiveIDs int
	PointerFree                                    bool
}
