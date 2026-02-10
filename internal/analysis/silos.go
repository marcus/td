// Package analysis provides code analysis utilities for td.
package analysis

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileOwnership tracks which sessions have worked on a file
type FileOwnership struct {
	FilePath string
	Authors  map[string]int // session_id -> count
	Count    int
	Critical bool // true if only 1 author
}

// AuthorContribution tracks an author's file coverage
type AuthorContribution struct {
	AuthorID     string
	FileCount    int
	CriticalRisk int // files where this author is sole contributor
	RatioOfAll   float64
}

// SuspiciousPattern tracks unusual knowledge distribution
type SuspiciousPattern struct {
	Pattern string
	Reason  string
	Severity string // "low", "medium", "high"
}

// SiloReport is the comprehensive silo analysis result
type SiloReport struct {
	FileOwnership      []FileOwnership
	AuthorContribution []AuthorContribution
	FileAuthorPairs    int // total unique (file, author) pairs
	IssueCoverage      int // issues with file attachments
	SuspiciousPatterns []SuspiciousPattern
	CriticalFiles      []string // files touched by only one person
	ExploredCodeRatio  float64  // percentage of codebase touched by issues
	TotalCodeFiles     int      // total files in repo
	SiloRiskScore      float64  // 0.0 to 1.0, higher = more risk
}

// AnalyzeSilos performs comprehensive silo analysis
func AnalyzeSilos(db *sql.DB, baseDir string) (*SiloReport, error) {
	report := &SiloReport{
		FileOwnership:      []FileOwnership{},
		AuthorContribution: []AuthorContribution{},
		SuspiciousPatterns: []SuspiciousPattern{},
		CriticalFiles:      []string{},
	}

	// Get all files tracked in issue_files
	fileData, err := getFileOwnershipData(db)
	if err != nil {
		return nil, fmt.Errorf("failed to get file ownership data: %w", err)
	}

	// Build FileOwnership map
	fileOwnershipMap := make(map[string]*FileOwnership)
	for filePath, authors := range fileData {
		fo := &FileOwnership{
			FilePath: filePath,
			Authors:  authors,
			Count:    len(authors),
			Critical: len(authors) == 1,
		}
		fileOwnershipMap[filePath] = fo
		report.FileOwnership = append(report.FileOwnership, *fo)
		report.FileAuthorPairs += fo.Count

		if fo.Critical {
			report.CriticalFiles = append(report.CriticalFiles, filePath)
		}
	}

	// Get issue coverage
	issueCoverageCount, err := getIssueCoverageCount(db)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue coverage: %w", err)
	}
	report.IssueCoverage = issueCoverageCount

	// Build author contributions
	authorStats := make(map[string]*AuthorContribution)
	for _, fo := range report.FileOwnership {
		for author := range fo.Authors {
			if _, exists := authorStats[author]; !exists {
				authorStats[author] = &AuthorContribution{
					AuthorID: author,
				}
			}
			authorStats[author].FileCount++
			if fo.Critical {
				authorStats[author].CriticalRisk++
			}
		}
	}

	// Convert to slice and calculate ratios
	totalFiles := len(fileOwnershipMap)
	for _, ac := range authorStats {
		ac.RatioOfAll = float64(ac.FileCount) / float64(totalFiles)
		report.AuthorContribution = append(report.AuthorContribution, *ac)
	}

	// Sort by file count descending
	sort.Slice(report.AuthorContribution, func(i, j int) bool {
		return report.AuthorContribution[i].FileCount > report.AuthorContribution[j].FileCount
	})

	// Analyze explored code ratio (requires filesystem scan)
	totalCodeFiles, err := countRepositoryFiles(baseDir)
	if err == nil {
		report.TotalCodeFiles = totalCodeFiles
		if totalCodeFiles > 0 {
			report.ExploredCodeRatio = float64(totalFiles) / float64(totalCodeFiles)
		}
	}

	// Detect suspicious patterns
	report.SuspiciousPatterns = detectPatterns(report)

	// Calculate risk score
	report.SiloRiskScore = calculateRiskScore(report)

	return report, nil
}

// getFileOwnershipData returns map of file -> {author_id -> count}
func getFileOwnershipData(db *sql.DB) (map[string]map[string]int, error) {
	fileData := make(map[string]map[string]int)

	rows, err := db.Query(`
		SELECT DISTINCT if.file_path, i.implementer_session
		FROM issue_files if
		JOIN issues i ON if.issue_id = i.id
		WHERE i.implementer_session != '' AND if.file_path != ''
		ORDER BY if.file_path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var filePath, session string
		if err := rows.Scan(&filePath, &session); err != nil {
			return nil, err
		}

		if fileData[filePath] == nil {
			fileData[filePath] = make(map[string]int)
		}
		fileData[filePath][session]++
	}

	return fileData, rows.Err()
}

// getIssueCoverageCount returns number of issues with file attachments
func getIssueCoverageCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(DISTINCT issue_id)
		FROM issue_files
		WHERE file_path != ''
	`).Scan(&count)
	return count, err
}

// countRepositoryFiles counts code files in the repository
func countRepositoryFiles(baseDir string) (int, error) {
	count := 0
	codeExtensions := map[string]bool{
		".go": true, ".rs": true, ".py": true, ".js": true, ".ts": true,
		".tsx": true, ".jsx": true, ".java": true, ".cpp": true, ".c": true,
		".h": true, ".rb": true, ".php": true, ".swift": true, ".kt": true,
		".scala": true, ".groovy": true, ".clj": true, ".erl": true, ".ex": true,
		".lua": true, ".pl": true, ".r": true, ".sql": true, ".sh": true,
	}

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		// Skip common directories
		if info.IsDir() {
			switch info.Name() {
			case ".git", ".todos", "node_modules", "vendor", ".cache", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}

		// Check if file has code extension
		ext := strings.ToLower(filepath.Ext(path))
		if codeExtensions[ext] {
			count++
		}

		return nil
	})

	return count, err
}

// detectPatterns analyzes knowledge distribution for suspicious patterns
func detectPatterns(report *SiloReport) []SuspiciousPattern {
	patterns := []SuspiciousPattern{}

	// Pattern 1: Files with only one author (critical knowledge silos)
	singleAuthorFiles := 0
	for _, fo := range report.FileOwnership {
		if fo.Critical {
			singleAuthorFiles++
		}
	}

	if singleAuthorFiles > 0 {
		ratio := float64(singleAuthorFiles) / float64(len(report.FileOwnership))
		severity := "low"
		if ratio > 0.3 {
			severity = "high"
		} else if ratio > 0.15 {
			severity = "medium"
		}

		patterns = append(patterns, SuspiciousPattern{
			Pattern:  fmt.Sprintf("%d files with single author", singleAuthorFiles),
			Reason:   fmt.Sprintf("%.1f%% of tracked files touched by only one developer", ratio*100),
			Severity: severity,
		})
	}

	// Pattern 2: Authors with disproportionate coverage
	if len(report.AuthorContribution) > 0 {
		topAuthor := report.AuthorContribution[0]
		avgCoverage := 1.0 / float64(len(report.AuthorContribution))

		if topAuthor.RatioOfAll > avgCoverage*3 {
			patterns = append(patterns, SuspiciousPattern{
				Pattern:  fmt.Sprintf("%s owns %.0f%% of tracked files", topAuthor.AuthorID[:8], topAuthor.RatioOfAll*100),
				Reason:   "Single developer has disproportionate file ownership",
				Severity: "high",
			})
		}
	}

	// Pattern 3: Low code exploration
	if report.ExploredCodeRatio > 0 && report.ExploredCodeRatio < 0.1 {
		patterns = append(patterns, SuspiciousPattern{
			Pattern:  fmt.Sprintf("Only %.1f%% of codebase is tracked by issues", report.ExploredCodeRatio*100),
			Reason:   "Large portions of codebase have no issue file linkage",
			Severity: "medium",
		})
	}

	// Pattern 4: High critical risk for top authors
	if len(report.AuthorContribution) > 0 {
		for _, ac := range report.AuthorContribution {
			if ac.CriticalRisk > 5 {
				patterns = append(patterns, SuspiciousPattern{
					Pattern:  fmt.Sprintf("%s is sole contributor on %d files", ac.AuthorID[:8], ac.CriticalRisk),
					Reason:   "High risk of knowledge concentration",
					Severity: "high",
				})
			}
		}
	}

	return patterns
}

// calculateRiskScore returns 0.0 to 1.0 risk score
func calculateRiskScore(report *SiloReport) float64 {
	if len(report.FileOwnership) == 0 {
		return 0.0
	}

	score := 0.0

	// Critical files ratio (max 0.4 points)
	criticalRatio := float64(len(report.CriticalFiles)) / float64(len(report.FileOwnership))
	score += criticalRatio * 0.4

	// Author concentration (max 0.3 points)
	if len(report.AuthorContribution) > 0 {
		topAuthorRatio := report.AuthorContribution[0].RatioOfAll
		if topAuthorRatio > 0.5 {
			score += 0.3
		} else if topAuthorRatio > 0.33 {
			score += 0.2
		} else if topAuthorRatio > 0.25 {
			score += 0.1
		}
	}

	// Explored code ratio (max 0.2 points)
	if report.TotalCodeFiles > 0 && report.ExploredCodeRatio < 0.1 {
		score += 0.2
	} else if report.ExploredCodeRatio < 0.2 {
		score += 0.1
	}

	// High critical risk count (max 0.1 points)
	totalCriticalRisk := 0
	for _, ac := range report.AuthorContribution {
		totalCriticalRisk += ac.CriticalRisk
	}
	if totalCriticalRisk > 10 {
		score += 0.1
	}

	if score > 1.0 {
		return 1.0
	}
	return score
}
