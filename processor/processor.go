package processor

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xDAFE/exifsorter/metadata"
)

type Processor struct {
	OutputDir string
	DryRun    bool
	Move      bool
	Verify    bool
	Offset    time.Duration
	Format    string
}

func (p *Processor) ProcessFiles(files []metadata.FileMetadata) error {
	for _, f := range files {
		if strings.ToLower(filepath.Ext(f.SourceFile)) == ".xmp" {
			continue // Handled as sidecars
		}

		if f.Error != "" {
			fmt.Printf("[SKIP] %s (Error: %s)\n", f.SourceFile, f.Error)
			continue
		}

		fi, err := os.Stat(f.SourceFile)
		if err != nil || fi.IsDir() {
			continue // Skip directories or unstatable files
		}

		t, method := extractBestDate(f)
		if t.IsZero() {
			fmt.Printf("[SKIP] %s (No valid date found)\n", f.SourceFile)
			continue
		}

		// Apply offset
		adjustedT := t.Add(p.Offset)
		
		// Generate destination path
		relPath := adjustedT.Format(p.Format)
		destDir := filepath.Join(p.OutputDir, relPath)
		destPath := filepath.Join(destDir, filepath.Base(f.SourceFile))

		// Resolve collisions
		destPath = resolveCollision(destPath, p.DryRun)

		// Check for XMP sidecar
		ext := filepath.Ext(f.SourceFile)
		baseWithoutExt := strings.TrimSuffix(f.SourceFile, ext)
		xmpSource := baseWithoutExt + ".xmp"
		hasXMP := false
		if _, err := os.Stat(xmpSource); err == nil {
			hasXMP = true
		} else {
			xmpSourceAlt := f.SourceFile + ".xmp"
			if _, err := os.Stat(xmpSourceAlt); err == nil {
				xmpSource = xmpSourceAlt
				hasXMP = true
			}
		}

		var xmpDest string
		if hasXMP {
			destBaseWithoutExt := strings.TrimSuffix(destPath, filepath.Ext(destPath))
			xmpDest = destBaseWithoutExt + ".xmp"
		}

		offsetStr := ""
		if p.Offset != 0 {
			offsetStr = fmt.Sprintf(", Offset: %s", p.Offset.String())
		}
		
		action := "Would copy"
		if p.Move {
			action = "Would move"
		}
		
		if p.DryRun {
			fmt.Printf("[DRY RUN] %s (Extracted: %s [%s]%s) -> %s to '%s'\n",
				filepath.Base(f.SourceFile),
				t.Format("2006-01-02 15:04:05"),
				method,
				offsetStr,
				action,
				destPath,
			)
			if hasXMP {
				fmt.Printf("[DRY RUN] Sidecar %s -> %s to '%s'\n", filepath.Base(xmpSource), action, xmpDest)
			}
			continue
		}

		// Perform action
		if err := os.MkdirAll(destDir, 0755); err != nil {
			fmt.Printf("[ERROR] Failed to create directory %s: %v\n", destDir, err)
			continue
		}

		p.executeAction(f.SourceFile, destPath)
		if hasXMP {
			p.executeAction(xmpSource, xmpDest)
		}
	}
	return nil
}

func (p *Processor) executeAction(src, dst string) {
	var srcHash string
	if p.Verify {
		var err error
		srcHash, err = hashFile(src)
		if err != nil {
			fmt.Printf("[ERROR] Failed to hash source %s: %v\n", src, err)
			return
		}
	}

	if p.Move {
		if err := moveFile(src, dst); err != nil {
			fmt.Printf("[ERROR] Failed to move %s: %v\n", src, err)
		} else {
			if p.Verify {
				dstHash, err := hashFile(dst)
				if err != nil || dstHash != srcHash {
					fmt.Printf("[ERROR] Verification failed for moved file %s\n", dst)
				} else {
					fmt.Printf("[MOVED] %s -> %s (Verified)\n", src, dst)
				}
			} else {
				fmt.Printf("[MOVED] %s -> %s\n", src, dst)
			}
		}
	} else {
		if err := copyFile(src, dst); err != nil {
			fmt.Printf("[ERROR] Failed to copy %s: %v\n", src, err)
		} else {
			if p.Verify {
				dstHash, err := hashFile(dst)
				if err != nil || dstHash != srcHash {
					fmt.Printf("[ERROR] Verification failed for copied file %s. Removing corrupted copy.\n", dst)
					os.Remove(dst)
				} else {
					fmt.Printf("[COPIED] %s -> %s (Verified)\n", src, dst)
				}
			} else {
				fmt.Printf("[COPIED] %s -> %s\n", src, dst)
			}
		}
	}
}

func extractBestDate(f metadata.FileMetadata) (time.Time, string) {
	candidates := []struct {
		Name  string
		Value string
	}{
		{"CreationDate", f.CreationDate},
		{"DateTimeOriginal", f.DateTimeOriginal},
		{"CreateDate", f.CreateDate},
		{"MediaCreateDate", f.MediaCreateDate},
		{"TrackCreateDate", f.TrackCreateDate},
		{"FileModifyDate", f.FileModifyDate},
	}

	for _, c := range candidates {
		if c.Value == "" || c.Value == "0000:00:00 00:00:00" {
			continue
		}
		if t, err := parseTime(c.Value); err == nil {
			return t, c.Name
		}
	}
	
	return time.Time{}, ""
}

var layouts = []string{
	"2006:01:02 15:04:05-07:00",
	"2006:01:02 15:04:05Z",
	"2006:01:02 15:04:05",
}

func parseTime(value string) (time.Time, error) {
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time format")
}

func resolveCollision(destPath string, dryRun bool) string {
	if dryRun {
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			return destPath
		}
	}
	
	dir := filepath.Dir(destPath)
	ext := filepath.Ext(destPath)
	base := strings.TrimSuffix(filepath.Base(destPath), ext)

	counter := 1
	newPath := destPath
	for {
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
		newPath = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, counter, ext))
		counter++
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	
	if info, err := os.Stat(src); err == nil {
		os.Chtimes(dst, info.ModTime(), info.ModTime())
	}
	
	return out.Sync()
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	
	if strings.Contains(err.Error(), "cross-device link") || strings.Contains(err.Error(), "EXDEV") {
		if err := copyFile(src, dst); err != nil {
			return err
		}
		return os.Remove(src)
	}
	return err
}
