package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type JSONData map[string]interface{}

func main() {
	// Command-line flags
	inDir := flag.String("i", "", "Input directory containing JSON trufflehog output files (required)")
	searchTerm := flag.String("s", "", "String to search for (required) (case-insensitive)")
	searchMode := flag.String("m", "contains", "Search mode: 'exact' or 'contains'")
	searchField := flag.String("f", "", "Specific field to search in (optional)")
	listFields := flag.Bool("l", false, "List all searchable fields (case-sensitive)")
	numThreads := flag.Int("t", 1, "Number of goroutines for parallel processing")
	flag.Parse()

	// Handle the -l flag to list all fields
	if *listFields {
		printSearchableFields()
		os.Exit(0)
	}

	// Validate flags
	if *inDir == "" {
		fmt.Println("Error: -i is a required parameter.")
		flag.Usage()
		os.Exit(1)
	}

	if *searchTerm == "" {
		fmt.Println("Error: -s is a required parameter.")
		flag.Usage()
		os.Exit(1)
	}

	if *searchMode != "exact" && *searchMode != "contains" {
		fmt.Println("Error: -m must be 'exact' or 'contains'.")
		os.Exit(1)
	}

	// Convert search term to lowercase for case-insensitive matching
	searchTermLower := strings.ToLower(*searchTerm)

	// Prefixes for Json search. Easier add or remove in case of structure changes
	fieldPrefixes := []string{"", "SourceMetadata.Data.Github."}

	// Read all JSON files from the directory
	files, err := os.ReadDir(*inDir)
	if err != nil {
		fmt.Printf("Error reading directory: %v\n", err)
		os.Exit(1)
	}

	// Create worker pool
	fileChan := make(chan os.DirEntry, len(files))
	var wg sync.WaitGroup

	// Launch worker goroutines
	for i := 0; i < *numThreads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range fileChan {
				if filepath.Ext(file.Name()) == ".json" {
					processFile(filepath.Join(*inDir, file.Name()), searchTermLower, *searchMode, *searchField, fieldPrefixes)
				}
			}
		}()
	}

	// Feed files into the channel
	for _, file := range files {
		fileChan <- file
	}
	close(fileChan)

	// Wait for all workers to complete
	wg.Wait()
}

// Process a single JSON file
func processFile(filePath, searchTerm, searchMode, searchField string, fieldPrefixes []string) {
	fileHandle, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error opening file %s: %v\n", filePath, err)
		return
	}
	defer fileHandle.Close()

	fmt.Printf("\n--- Searching in file: %s ---\n", filepath.Base(filePath))
	scanner := bufio.NewScanner(fileHandle)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		var jsonData JSONData
		err := json.Unmarshal([]byte(line), &jsonData)
		if err != nil {
			fmt.Printf("Error parsing JSON at line %d in file %s: %v\n", lineNum, filepath.Base(filePath), err)
			continue
		}

		// Attempt search with each prefix
		found := false
		for _, prefix := range fieldPrefixes {
			fullField := prefix + searchField
			if match := findAndPrintRelatedData(jsonData, searchTerm, searchMode, fullField); match {
				fmt.Printf("\n--- Related Data at line %d ---\n", lineNum)
				printPrettyJSON(jsonData)
				found = true
				break
			}
		}

		if !found && searchField == "" {
			// Search the entire JSON if no specific field is specified
			if match := findAndPrintRelatedData(jsonData, searchTerm, searchMode, ""); match {
				fmt.Printf("\n--- Related Data at line %d ---\n", lineNum)
				printPrettyJSON(jsonData)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Error reading file %s: %v\n", filepath.Base(filePath), err)
	}
}

// Print all searchable fields
func printSearchableFields() {
	fields := []string{
		"DecoderName", "DetectorDescription", "DetectorName", "DetectorType", "project", "rotation_guide",
		"Raw", "RawV2", "Redacted", "SourceID", "commit", "email", "file", "line", "link",
		"repository", "timestamp", "SourceName", "SourceType", "StructuredData", "VerificationFromCache", "Verified",
	}

	fmt.Println("Searchable Fields (case-sensitive):")
	fmt.Println(strings.Repeat("-", 40))
	for _, field := range fields {
		fmt.Printf("- %s\n", field)
	}
	fmt.Println(strings.Repeat("-", 40))
}

// Search for the term and determine if related data should be printed
func findAndPrintRelatedData(data JSONData, term, mode, field string) bool {
	if field != "" {
		if value, exists := getNestedField(data, field); exists {
			return checkMatch(value, term, mode)
		}
		return false
	}

	// Search the entire JSON if no specific field is specified
	for _, value := range data {
		if checkMatch(value, term, mode) {
			return true
		}
	}
	return false
}

// Check if a value matches the search term based on the mode
func checkMatch(value interface{}, term, mode string) bool {
	switch v := value.(type) {
	case string:
		lowerValue := strings.ToLower(v) // Convert to lowercase for case-insensitive matching
		if (mode == "exact" && lowerValue == term) || (mode == "contains" && strings.Contains(lowerValue, term)) {
			return true
		}
	case []interface{}:
		for _, item := range v {
			if checkMatch(item, term, mode) {
				return true
			}
		}
	case map[string]interface{}:
		for _, item := range v {
			if checkMatch(item, term, mode) {
				return true
			}
		}
	}
	return false
}

// Get a nested field value by path (e.g., "a.b.c")
func getNestedField(data JSONData, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	current := data
	for i, part := range parts {
		value, exists := current[part]
		if !exists {
			return nil, false
		}
		if i == len(parts)-1 {
			return value, true
		}
		subMap, ok := value.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = subMap
	}
	return nil, false
}

// Print the JSON object in a pretty format
func printPrettyJSON(data JSONData) {
	prettyData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("Error pretty-printing JSON: %v\n", err)
		return
	}
	fmt.Println(string(prettyData))
}
