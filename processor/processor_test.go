package processor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xDAFE/exifsorter/metadata"
)

func TestExtractBestDate_PriorityAndFallbacks(t *testing.T) {
	tests := []struct {
		name       string
		meta       metadata.FileMetadata
		wantMethod string
		wantTime   string
		wantZero   bool
	}{
		{
			name: "prefers CreationDate over all other fields",
			meta: metadata.FileMetadata{
				CreationDate:     "2023:05:01 10:00:00Z",
				DateTimeOriginal: "2023:05:02 10:00:00",
				CreateDate:       "2023:05:03 10:00:00",
				MediaCreateDate:  "2023:05:04 10:00:00",
				TrackCreateDate:  "2023:05:05 10:00:00",
				FileModifyDate:   "2023:05:06 10:00:00",
			},
			wantMethod: "CreationDate",
			wantTime:   "2023-05-01 10:00:00",
		},
		{
			name: "skips zero EXIF placeholder and malformed higher-priority dates",
			meta: metadata.FileMetadata{
				CreationDate:     "0000:00:00 00:00:00",
				DateTimeOriginal: "not-a-valid-date",
				CreateDate:       "2022:11:15 08:30:45+02:00",
				FileModifyDate:   "2023:01:01 00:00:00",
			},
			wantMethod: "CreateDate",
			wantTime:   "2022-11-15 08:30:45",
		},
		{
			name: "falls back through MediaCreateDate, TrackCreateDate, to FileModifyDate",
			meta: metadata.FileMetadata{
				MediaCreateDate: "0000:00:00 00:00:00",
				TrackCreateDate: "",
				FileModifyDate:  "2021:03:20 19:15:00-05:00",
			},
			wantMethod: "FileModifyDate",
			wantTime:   "2021-03-20 19:15:00",
		},
		{
			name: "returns zero time when all date fields are empty or invalid",
			meta: metadata.FileMetadata{
				CreationDate:     "0000:00:00 00:00:00",
				DateTimeOriginal: "invalid",
			},
			wantZero: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTime, gotMethod := extractBestDate(tc.meta)
			if tc.wantZero {
				if !gotTime.IsZero() || gotMethod != "" {
					t.Fatalf("expected zero time and empty method, got (%v, %q)", gotTime, gotMethod)
				}
				return
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("expected method %q, got %q", tc.wantMethod, gotMethod)
			}
			if got := gotTime.Format("2006-01-02 15:04:05"); got != tc.wantTime {
				t.Errorf("expected formatted time %q, got %q", tc.wantTime, got)
			}
		})
	}
}

func TestResolveCollision(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "IMG_1001.jpg")

	// No existing file -> returns original path
	if got := resolveCollision(target, false); got != target {
		t.Fatalf("expected %q when no collision exists, got %q", target, got)
	}

	// Create original file -> should resolve to _1
	if err := os.WriteFile(target, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	want1 := filepath.Join(tmpDir, "IMG_1001_1.jpg")
	if got := resolveCollision(target, false); got != want1 {
		t.Fatalf("expected %q on first collision, got %q", want1, got)
	}

	// Create _1 file -> should resolve to _2 (even in dryRun mode when collisions exist on disk)
	if err := os.WriteFile(want1, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	want2 := filepath.Join(tmpDir, "IMG_1001_2.jpg")
	if got := resolveCollision(target, true); got != want2 {
		t.Fatalf("expected %q on second collision in dry-run, got %q", want2, got)
	}
}

func TestProcessFiles_OffsetCrossesYearBoundaryAndHandlesSidecars(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()

	// Primary image + basename.xmp sidecar
	img1 := filepath.Join(srcDir, "DSC0001.JPG")
	xmp1 := filepath.Join(srcDir, "DSC0001.xmp")
	if err := os.WriteFile(img1, []byte("image-1-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmp1, []byte("xmp-1-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	// Second image with alternative sidecar naming (<filename>.<ext>.xmp)
	img2 := filepath.Join(srcDir, "DSC0002.RAW")
	xmp2 := filepath.Join(srcDir, "DSC0002.RAW.xmp")
	if err := os.WriteFile(img2, []byte("image-2-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmp2, []byte("xmp-2-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	proc := &Processor{
		OutputDir: outDir,
		DryRun:    false,
		Move:      false,
		Verify:    true,
		Offset:    3 * time.Hour, // Shifts 2023-12-31 22:30 -> 2024-01-01 01:30
		Format:    "2006/01/02",
	}

	files := []metadata.FileMetadata{
		// Standalone .xmp in input list should be ignored (handled via sidecar logic)
		{
			SourceFile:     xmp1,
			FileModifyDate: "2020:01:01 00:00:00",
		},
		{
			SourceFile:       img1,
			DateTimeOriginal: "2023:12:31 22:30:00",
		},
		{
			SourceFile:       img2,
			DateTimeOriginal: "2023:12:31 22:30:00",
		},
	}

	if err := proc.ProcessFiles(files); err != nil {
		t.Fatalf("ProcessFiles returned unexpected error: %v", err)
	}

	expectedDir := filepath.Join(outDir, "2024", "01", "01")
	for _, relName := range []string{"DSC0001.JPG", "DSC0001.xmp", "DSC0002.RAW", "DSC0002.xmp"} {
		fullPath := filepath.Join(expectedDir, relName)
		if _, err := os.Stat(fullPath); err != nil {
			t.Errorf("expected sorted file %q to exist: %v", fullPath, err)
		}
	}

	// Ensure standalone .xmp was NOT sorted into 2020/01/01
	unexpectedXMP := filepath.Join(outDir, "2020", "01", "01", "DSC0001.xmp")
	if _, err := os.Stat(unexpectedXMP); !os.IsNotExist(err) {
		t.Errorf("standalone .xmp should be skipped from independent sorting, but found %q", unexpectedXMP)
	}
}

func TestProcessFiles_CollisionRenamesBothMediaAndSidecar(t *testing.T) {
	srcDirA := t.TempDir()
	srcDirB := t.TempDir()
	outDir := t.TempDir()

	// Two files with the same filename in different source folders and same capture date
	imgA := filepath.Join(srcDirA, "photo.jpg")
	imgB := filepath.Join(srcDirB, "photo.jpg")
	xmpB := filepath.Join(srcDirB, "photo.xmp")

	if err := os.WriteFile(imgA, []byte("content-A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imgB, []byte("content-B"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmpB, []byte("sidecar-B"), 0644); err != nil {
		t.Fatal(err)
	}

	proc := &Processor{
		OutputDir: outDir,
		Move:      true,
		Verify:    true,
		Format:    "2006-01-02",
	}

	files := []metadata.FileMetadata{
		{SourceFile: imgA, DateTimeOriginal: "2024:06:15 12:00:00"},
		{SourceFile: imgB, DateTimeOriginal: "2024:06:15 18:00:00"},
	}

	if err := proc.ProcessFiles(files); err != nil {
		t.Fatalf("ProcessFiles failed: %v", err)
	}

	destDir := filepath.Join(outDir, "2024-06-15")
	gotA, err := os.ReadFile(filepath.Join(destDir, "photo.jpg"))
	if err != nil || string(gotA) != "content-A" {
		t.Fatalf("expected photo.jpg with content-A, got %q (err=%v)", string(gotA), err)
	}

	gotB, err := os.ReadFile(filepath.Join(destDir, "photo_1.jpg"))
	if err != nil || string(gotB) != "content-B" {
		t.Fatalf("expected collided photo_1.jpg with content-B, got %q (err=%v)", string(gotB), err)
	}

	// The sidecar for the collided photo.jpg must also be renamed to photo_1.xmp
	gotXMP, err := os.ReadFile(filepath.Join(destDir, "photo_1.xmp"))
	if err != nil || string(gotXMP) != "sidecar-B" {
		t.Fatalf("expected collided sidecar photo_1.xmp with sidecar-B, got %q (err=%v)", string(gotXMP), err)
	}

	// Since Move=true, original files should have been removed
	if _, err := os.Stat(imgA); !os.IsNotExist(err) {
		t.Errorf("expected source %q to be removed after move", imgA)
	}
	if _, err := os.Stat(imgB); !os.IsNotExist(err) {
		t.Errorf("expected source %q to be removed after move", imgB)
	}
	if _, err := os.Stat(xmpB); !os.IsNotExist(err) {
		t.Errorf("expected sidecar %q to be removed after move", xmpB)
	}
}

func TestProcessFiles_DryRunAndSkipConditions(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()

	validImg := filepath.Join(srcDir, "valid.jpg")
	errImg := filepath.Join(srcDir, "errored.jpg")
	noDateImg := filepath.Join(srcDir, "nodate.jpg")

	for _, p := range []string{validImg, errImg, noDateImg} {
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// First test DryRun: nothing should be created in outDir and source should remain
	dryProc := &Processor{
		OutputDir: outDir,
		DryRun:    true,
		Move:      true,
		Format:    "2006/01/02",
	}
	if err := dryProc.ProcessFiles([]metadata.FileMetadata{
		{SourceFile: validImg, DateTimeOriginal: "2024:02:10 10:00:00"},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected output directory to remain empty during dry-run, found %d entries", len(entries))
	}
	if _, err := os.Stat(validImg); err != nil {
		t.Fatalf("expected source file to remain untouched during dry-run: %v", err)
	}

	// Second test skip conditions (Error field set, no valid date, directory, missing file)
	realProc := &Processor{
		OutputDir: outDir,
		DryRun:    false,
		Move:      false,
		Format:    "2006/01/02",
	}
	if err := realProc.ProcessFiles([]metadata.FileMetadata{
		{SourceFile: errImg, DateTimeOriginal: "2024:02:10 10:00:00", Error: "File format error"},
		{SourceFile: noDateImg},
		{SourceFile: srcDir, DateTimeOriginal: "2024:02:10 10:00:00"},
		{SourceFile: filepath.Join(srcDir, "missing.jpg"), DateTimeOriginal: "2024:02:10 10:00:00"},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err = os.ReadDir(outDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected all invalid/errored/directory files to be skipped, found %d entries", len(entries))
	}
}
