package main

// Business configuration exports exclude panel account identities. Internal
// audit history retains actors; portable configuration archives do not.
func stripArchiveActors(tables []tableDump) {
	for i := range tables {
		table := &tables[i]
		if table.Name != "config_revisions" {
			continue
		}
		for column, name := range table.Columns {
			if name != "actor" {
				continue
			}
			for row := range table.Rows {
				if column < len(table.Rows[row]) {
					table.Rows[row][column] = configCell{Type: "text", Value: ""}
				}
			}
		}
	}
}
