package ats

import (
	"github.com/parquet-go/parquet-go"
	"os"
)

func WriteRecords[T any](path string, records []T) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var zero T
	w := parquet.NewWriter(f, parquet.SchemaOf(&zero), parquet.Compression(&parquet.Snappy))
	defer w.Close()

	for _, rec := range records {
		recCopy := rec // copy to get a new pointer as Write takes the address.
		if err := w.Write(&recCopy); err != nil {
			return err
		}
	}
	return nil
}
