package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/0xDAFE/exifsorter/metadata"
	"github.com/0xDAFE/exifsorter/processor"
)

func main() {
	inputDir := flag.String("input", "", "Input directory (required)")
	outputDir := flag.String("output", "", "Output directory (required)")
	dryRun := flag.Bool("dry-run", false, "Perform a dry run without copying/moving files")
	move := flag.Bool("move", false, "Move files instead of copying them")
	verify := flag.Bool("verify", false, "Verify data integrity using SHA256 hashes after copy/move")
	offsetStr := flag.String("offset", "", "Optional time offset to apply (e.g., '+2h', '-4h30m')")
	format := flag.String("format", "2006/01/02", "Output folder format in Go time format")

	flag.Parse()

	if *inputDir == "" || *outputDir == "" {
		fmt.Println("Error: -input and -output are required.")
		flag.Usage()
		os.Exit(1)
	}

	var offset time.Duration
	if *offsetStr != "" {
		cleanOffset := *offsetStr
		if cleanOffset[0] == '+' {
			cleanOffset = cleanOffset[1:]
		}
		
		var err error
		offset, err = time.ParseDuration(cleanOffset)
		if err != nil {
			fmt.Printf("Error parsing offset: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("Extracting metadata with exiftool...")
	files, err := metadata.ExtractMetadata(*inputDir)
	if err != nil {
		fmt.Printf("Error extracting metadata: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Processing %d files...\n", len(files))

	proc := &processor.Processor{
		OutputDir: *outputDir,
		DryRun:    *dryRun,
		Move:      *move,
		Verify:    *verify,
		Offset:    offset,
		Format:    *format,
	}

	if err := proc.ProcessFiles(files); err != nil {
		fmt.Printf("Error processing files: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Done.")
}
