package store

// sequentialTestRows gives legacy test fixtures explicit runtime-issued seqs.
// Production callers must provide these identities before reaching Store.
func sequentialTestRows(rows []Event) []Event {
	next := uint64(0)
	for index := range rows {
		if rows[index].Seq == 0 {
			next++
			rows[index].Seq = next
			continue
		}
		next = rows[index].Seq
	}
	return rows
}
