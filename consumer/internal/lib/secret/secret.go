package secret

import (
	"io"
	"log"
	"os"
)

func Read(path string) []byte {
	file, err := os.Open(path)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	defer file.Close()

	data := make([]byte, 64)

	for {
		n, err := file.Read(data)
		if err == io.EOF { // если конец файла
			break // выходим из цикла
		}
		data = data[:n]
	}
	return data
}
