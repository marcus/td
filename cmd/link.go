package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/dependency"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

// computeFileSHA computes SHA256 hash of a file
func computeFileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// countFileLines counts the number of lines in a file
func countFileLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	lines := 1
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	return lines
}

// getGitModifiedFiles returns files modified in git working tree
func getGitModifiedFiles() ([]string, []string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, err
	}

	var modified, untracked []string
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		status := line[:2]
		file := strings.TrimSpace(line[3:])
		// Handle renamed files (format: "old -> new")
		if strings.Contains(file, " -> ") {
			parts := strings.Split(file, " -> ")
			file = parts[1]
		}

		if status == "??" {
			untracked = append(untracked, file)
		} else {
			modified = append(modified, file)
		}
	}

	return modified, untracked, nil
}

var linkCmd = &cobra.Command{
	Use:   "link [issue-id] [file-pattern...]",
	Short: "Link files to an issue",
	Long: `Link one or more files to an issue.

Examples:
  td link td-abc1 src/main.go           # Link single file
  td link td-abc1 src/*.go              # Link via glob pattern
  td link td-abc1 file1.go file2.go     # Link multiple files
  td link td-abc1 --depends-on td-xyz   # Add dependency (alternative to 'td dep')`,
	GroupID: "files",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		issueID := args[0]

		// Handle --depends-on flag
		dependsOnID, _ := cmd.Flags().GetString("depends-on")
		if dependsOnID != "" {
			// Add dependency instead of linking files
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Error("issue not found: %s", issueID)
				return err
			}

			depIssue, err := database.GetIssue(dependsOnID)
			if err != nil {
				output.Error("issue not found: %s", dependsOnID)
				return err
			}

			// Validate and add with atomic logging
			err = dependency.Validate(database, issueID, dependsOnID)
			if err == dependency.ErrDependencyExists {
				output.Warning("%s already depends on %s", issueID, dependsOnID)
				return nil
			}
			if err != nil {
				output.Error("%v", err)
				return err
			}
			if err := database.AddDependencyLogged(issueID, dependsOnID, "depends_on", sess.ID); err != nil {
				output.Error("%v", err)
				return err
			}

			fmt.Printf("ADDED: %s depends on %s\n", issue.ID, depIssue.ID)
			fmt.Printf("  %s: %s\n", issue.ID, issue.Title)
			fmt.Printf("  └── now depends on: %s: %s\n", depIssue.ID, depIssue.Title)

			return nil
		}

		// Regular file linking (requires at least 2 args: issue + files)
		if len(args) < 2 {
			return fmt.Errorf("need file patterns or --depends-on flag")
		}

		patterns := args[1:]

		// Verify issue exists
		_, err = database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Get role
		roleStr, _ := cmd.Flags().GetString("role")
		role := models.FileRoleImplementation
		if roleStr != "" {
			role = models.FileRole(roleStr)
		}

		// Find matching files from all patterns
		var matches []string
		for _, pattern := range patterns {
			globMatches, err := filepath.Glob(pattern)
			if err != nil {
				output.Warning("invalid pattern: %s", pattern)
				continue
			}

			if len(globMatches) > 0 {
				matches = append(matches, globMatches...)
			} else {
				// Try as a literal path
				if _, err := os.Stat(pattern); err == nil {
					matches = append(matches, pattern)
				} else {
					output.Warning("no files matching: %s", pattern)
				}
			}
		}

		if len(matches) == 0 {
			output.Error("no files found matching any pattern")
			return fmt.Errorf("no matches")
		}

		// Handle directories
		recursive, _ := cmd.Flags().GetBool("recursive")
		var allFiles []string

		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}

			if info.IsDir() {
				if recursive {
					filepath.Walk(match, func(path string, info os.FileInfo, err error) error {
						if err != nil {
							return nil
						}
						if !info.IsDir() {
							allFiles = append(allFiles, path)
						}
						return nil
					})
				} else {
					// Just files in the directory
					entries, _ := os.ReadDir(match)
					for _, entry := range entries {
						if !entry.IsDir() {
							allFiles = append(allFiles, filepath.Join(match, entry.Name()))
						}
					}
				}
			} else {
				allFiles = append(allFiles, match)
			}
		}

		// Link each file
		count := 0
		for _, file := range allFiles {
			// Get absolute path for SHA computation
			absPath, _ := filepath.Abs(file)

			// Convert to repo-relative path for storage
			relPath, err := db.ToRepoRelative(absPath, baseDir)
			if err != nil {
				output.Warning("skipping %s: %v", file, err)
				continue
			}

			// Compute file SHA for change detection
			sha, err := computeFileSHA(absPath)
			if err != nil {
				output.Warning("failed to compute SHA for %s: %v", file, err)
				sha = "" // Store empty SHA, will be treated as "new"
			}

			if err := database.LinkFileLogged(issueID, relPath, role, sha, sess.ID); err != nil {
				output.Warning("failed to link %s: %v", file, err)
				continue
			}

			count++
		}

		if count == 1 {
			fmt.Printf("LINKED 1 file to %s\n", issueID)
		} else {
			fmt.Printf("LINKED %d files to %s\n", count, issueID)
		}

		return nil
	},
}

var unlinkCmd = &cobra.Command{
	Use:     "unlink [issue-id] [file-pattern]",
	Short:   "Remove file associations",
	GroupID: "files",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		issueID := args[0]
		pattern := args[1]

		// Get linked files
		files, err := database.GetLinkedFiles(issueID)
		if err != nil {
			output.Error("failed to get linked files: %v", err)
			return err
		}

		count := 0
		for _, file := range files {
			matched, _ := filepath.Match(pattern, file.FilePath)
			if matched || file.FilePath == pattern {
				if err := database.UnlinkFileLogged(issueID, file.FilePath, sess.ID); err != nil {
					output.Warning("failed to unlink %s: %v", file.FilePath, err)
					continue
				}
				count++
			}
		}

		if count == 1 {
			fmt.Printf("UNLINKED 1 file from %s\n", issueID)
		} else {
			fmt.Printf("UNLINKED %d files from %s\n", count, issueID)
		}

		return nil
	},
}

var filesCmd = &cobra.Command{
	Use:     "files [issue-id]",
	Short:   "List linked files with change status",
	GroupID: "files",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		issueID := args[0]

		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		files, err := database.GetLinkedFiles(issueID)
		if err != nil {
			output.Error("failed to get linked files: %v", err)
			return err
		}

		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			return output.JSON(files)
		}

		fmt.Printf("%s: %s\n", issue.ID, issue.Title)

		// Get start snapshot
		startSnapshot, _ := database.GetStartSnapshot(issueID)
		if startSnapshot != nil {
			fmt.Printf("Started: %s (%s)\n", output.ShortSHA(startSnapshot.CommitSHA), output.FormatTimeAgo(startSnapshot.Timestamp))
		}
		fmt.Println()

		// Group by role
		byRole := make(map[models.FileRole][]models.IssueFile)
		for _, f := range files {
			byRole[f.Role] = append(byRole[f.Role], f)
		}

		roles := []models.FileRole{
			models.FileRoleImplementation,
			models.FileRoleTest,
			models.FileRoleReference,
			models.FileRoleConfig,
		}

		changedOnly, _ := cmd.Flags().GetBool("changed")

		for _, role := range roles {
			roleFiles := byRole[role]
			if len(roleFiles) == 0 {
				continue
			}

			fmt.Printf("%s:\n", string(role))
			for _, f := range roleFiles {
				// Resolve to absolute path for file operations.
				// Stored paths are repo-relative; resolve against baseDir.
				absPath := f.FilePath
				if !filepath.IsAbs(f.FilePath) {
					absPath = filepath.Join(baseDir, filepath.FromSlash(f.FilePath))
				}

				// Check file status by comparing SHA
				status := "[unchanged]"
				lineStats := ""

				info, err := os.Stat(absPath)
				if os.IsNotExist(err) {
					status = "[deleted]"
				} else if err == nil {
					if f.LinkedSHA == "" {
						// No SHA stored - treat as new file
						status = "[new]"
						lineStats = fmt.Sprintf("+%d", countFileLines(absPath))
					} else {
						// Compare SHA
						currentSHA, err := computeFileSHA(absPath)
						if err != nil {
							status = "[error]"
						} else if currentSHA != f.LinkedSHA {
							status = "[modified]"
							// Compute line diff (simplified: just show current line count)
							lines := countFileLines(absPath)
							lineStats = fmt.Sprintf("~%d lines", lines)
						}
					}
					_ = info // Silence unused warning
				}

				if changedOnly && status == "[unchanged]" {
					continue
				}

				// Display the repo-relative path (already stored that way)
				displayPath := f.FilePath

				if lineStats != "" {
					fmt.Printf("  %-40s %-12s %s\n", displayPath, status, lineStats)
				} else {
					fmt.Printf("  %-40s %s\n", displayPath, status)
				}
			}
			fmt.Println()
		}

		if len(files) == 0 {
			fmt.Println("No linked files")
		}

		// Show untracked changes (files modified in git but not linked to this issue)
		showUntracked, _ := cmd.Flags().GetBool("untracked")
		if showUntracked {
			modified, untracked, err := getGitModifiedFiles()
			if err == nil {
				// Build set of linked file paths (repo-relative, forward slashes)
				linkedPaths := make(map[string]bool)
				for _, f := range files {
					linkedPaths[f.FilePath] = true
					// Also index with OS separators for matching
					linkedPaths[filepath.FromSlash(f.FilePath)] = true
				}

				// Filter out files that are already linked
				var untrackedModified, untrackedNew []string
				for _, f := range modified {
					// git status returns repo-relative paths
					normalized := filepath.ToSlash(f)
					if !linkedPaths[f] && !linkedPaths[normalized] {
						untrackedModified = append(untrackedModified, f)
					}
				}
				for _, f := range untracked {
					normalized := filepath.ToSlash(f)
					if !linkedPaths[f] && !linkedPaths[normalized] {
						untrackedNew = append(untrackedNew, f)
					}
				}

				if len(untrackedModified) > 0 || len(untrackedNew) > 0 {
					fmt.Println("UNTRACKED CHANGES (not linked to this issue):")
					for _, f := range untrackedModified {
						fmt.Printf("  %-40s [modified]\n", f)
					}
					for _, f := range untrackedNew {
						fmt.Printf("  %-40s [new]\n", f)
					}
					fmt.Println()
					fmt.Printf("Use `td link %s <file>` to associate these files.\n", issueID)
				}
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(linkCmd)
	rootCmd.AddCommand(unlinkCmd)
	rootCmd.AddCommand(filesCmd)

	linkCmd.Flags().String("role", "implementation", "File role: implementation, test, reference, config")
	linkCmd.Flags().Bool("recursive", true, "Include subdirectories")
	linkCmd.Flags().String("depends-on", "", "Add dependency instead of linking files (alternative to 'td dep')")

	filesCmd.Flags().Bool("json", false, "JSON output")
	filesCmd.Flags().Bool("changed", false, "Only show changed files")
	filesCmd.Flags().BoolP("untracked", "u", false, "Show untracked git changes not linked to this issue")
}
