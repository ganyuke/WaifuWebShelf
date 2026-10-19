package db

// testEdition makes a work of the given medium with one edition, for tests.
func testEdition(d *DB, seriesID int64, kind string, ed EditionWrite) (int64, error) {
	workID, err := d.CreateWork(seriesID, WorkWrite{Kind: kind})
	if err != nil {
		return 0, err
	}
	return d.CreateEdition(workID, ed)
}
