package metadata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

type FileMetadata struct {
	SourceFile       string `json:"SourceFile"`
	DateTimeOriginal string `json:"DateTimeOriginal"`
	CreationDate     string `json:"CreationDate"`
	CreateDate       string `json:"CreateDate"`
	MediaCreateDate  string `json:"MediaCreateDate"`
	TrackCreateDate  string `json:"TrackCreateDate"`
	FileModifyDate   string `json:"FileModifyDate"`
	Error            string `json:"Error"`
}

func ExtractMetadata(inputDir string) ([]FileMetadata, error) {
	cmd := exec.Command("exiftool", "-json", "-DateTimeOriginal", "-CreationDate", "-CreateDate", "-MediaCreateDate", "-TrackCreateDate", "-FileModifyDate", "-r", inputDir)

	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	_ = cmd.Run() // Ignore error as it might be non-zero for unreadable files

	if len(outb.Bytes()) == 0 {
		return nil, fmt.Errorf("no output from exiftool. Stderr: %s", errb.String())
	}

	var metadata []FileMetadata
	if err := json.Unmarshal(outb.Bytes(), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse exiftool output: %v", err)
	}

	return metadata, nil
}
