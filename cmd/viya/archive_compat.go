package main

// Old v3 exports contained the removed monitoring feature. Drop only those
// known tables; unknown tables must still fail normal archive validation.
// schema.go no longer recreates them: filtering here is what keeps pre-removal
// archives importable, so a fresh install does not carry empty dead tables.
func activeArchiveTables(tables []tableDump) []tableDump {
	active := make([]tableDump, 0, len(tables))
	for _, table := range tables {
		// config_revisions was removed: archives exported by older builds may
		// still contain it, so silently drop it instead of rejecting the import.
		if table.Name != "monitors" && table.Name != "monitor_samples" && table.Name != "config_revisions" {
			active = append(active, table)
		}
	}
	return active
}
