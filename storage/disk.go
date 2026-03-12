package storage

import (
	"os"
)

func SaveData1(path string, data []byte) error {

	// 1. Open file
	fp, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0644,
	)
	if err != nil {
		return err
	}

	defer fp.Close()

	// 2. Write the data
	_, err = fp.Write(data)
	if err != nil {
		return err
	}

	// 3. Flush data to disk
	err = fp.Sync()
	if err != nil {
		return err
	}

	return nil
}
