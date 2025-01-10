package kinesis_consumer

// noopStore implements the storage interface with discard
type noopStore struct{}

func (noopStore) SetCheckpoint(_, _, _ string) error        { return nil }
func (noopStore) GetCheckpoint(_, _ string) (string, error) { return "", nil }
