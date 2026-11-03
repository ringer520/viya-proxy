package main

// Old v3 exports contained the removed monitoring feature. Drop only those
// known tables; unknown tables must still fail normal archive validation.
func activeArchiveTables(tables []tableDump) []tableDump {
	active := make([]tableDump, 0, len(tables))
	for _, table := range tables {
		if table.Name != "monitors" && table.Name != "monitor_samples" {
			active = append(active, table)
		}
	}
	return active
}
