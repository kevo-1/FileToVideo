package temp

import (
	"fmt"
	"os"
	"strings"

	"github.com/kevo-1/FileToVideo/internal/repository"
)

func FileToBinary(tempFile *repository.TempFile) error {
	data, err := tempFile.Read()
	if err != nil {
		return fmt.Errorf("Failed to read file: %s", err)
	}

	var bitString strings.Builder
	for _, b := range data {
		fmt.Fprintf(&bitString, "%08b ", b)
	}

	binaryText := []byte(bitString.String())

	err = os.WriteFile(fmt.Sprintf("%s.bin", tempFile.FileName), binaryText, repository.OwnerRW)
	if err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}
