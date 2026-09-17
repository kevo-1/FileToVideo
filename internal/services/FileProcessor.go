package services

import (
	"fmt"
	"strings"

	"github.com/kevo-1/FileToVideo/internal/repository"
)

func ProcessFile(tempFile repository.TempFile) error {
	// First we process the file into binary
	data, err := tempFile.Read()

	if err != nil {
		return fmt.Errorf("Failed to read temporary file while processing")
	}

	// Process File to Binary
	binaryData, err := FileToBinary(data)
	if err != nil {
		return fmt.Errorf("Failed to convert file to binary")
	}

	// Place holder for when the image construction begins
	fmt.Printf("Done processing to binary: %s\n", string(binaryData))
	return nil
}

func FileToBinary(data []byte) ([]byte, error) {
	var bitString strings.Builder
	for _, b := range data {
		fmt.Fprintf(&bitString, "%08b ", b)
	}

	binaryText := []byte(bitString.String())

	return binaryText, nil
}
