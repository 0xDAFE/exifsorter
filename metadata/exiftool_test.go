package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeExiftool(t *testing.T, scriptBody string) {
	t.Helper()
	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "exiftool")
	content := "#!/bin/sh\n" + scriptBody + "\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		t.Fatalf("failed to create fake exiftool: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestExtractMetadata_ParsesJSONEvenOnNonZeroExit(t *testing.T) {
	// exiftool returns non-zero exit status when some files have warnings/errors,
	// but still prints valid JSON for all inspected files.
	writeFakeExiftool(t, `cat << 'EOF'
[
  {
    "SourceFile": "/tmp/photo.jpg",
    "DateTimeOriginal": "2023:07:14 15:09:26",
    "CreationDate": ""
  },
  {
    "SourceFile": "/tmp/corrupt.jpg",
    "Error": "Unknown file type"
  }
]
EOF
exit 1`)

	got, err := ExtractMetadata("/tmp")
	if err != nil {
		t.Fatalf("expected no error when exiftool returns valid JSON with exit code 1, got: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 metadata records, got %d", len(got))
	}
	if got[0].SourceFile != "/tmp/photo.jpg" || got[0].DateTimeOriginal != "2023:07:14 15:09:26" {
		t.Errorf("unexpected first record: %+v", got[0])
	}
	if got[1].Error != "Unknown file type" {
		t.Errorf("expected Error field to be preserved on second record, got: %+v", got[1])
	}
}

func TestExtractMetadata_EmptyOutputReturnsStderrError(t *testing.T) {
	writeFakeExiftool(t, `echo "File not found: /nonexistent" >&2
exit 1`)

	_, err := ExtractMetadata("/nonexistent")
	if err == nil {
		t.Fatal("expected error when exiftool produces no stdout")
	}
	if !strings.Contains(err.Error(), "File not found: /nonexistent") {
		t.Errorf("expected error to include stderr output, got: %v", err)
	}
}

func TestExtractMetadata_InvalidJSONReturnsError(t *testing.T) {
	writeFakeExiftool(t, `echo "not-json-output"`)

	_, err := ExtractMetadata("/tmp")
	if err == nil {
		t.Fatal("expected error when exiftool outputs invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse exiftool output") {
		t.Errorf("unexpected error message: %v", err)
	}
}
