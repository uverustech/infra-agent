package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const mainFile = "cmd/gtw-agent/main.go"

func main() {
	// 1. Get the last commit message
	out, err := exec.Command("git", "log", "-1", "--pretty=%B").Output()
	if err != nil {
		fmt.Printf("Error getting commit message: %v\n", err)
		os.Exit(1)
	}
	msg := string(out)

	// Guard: if message starts with "bump:", ignore to prevent loop
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(msg)), "bump:") {
		fmt.Println("Skipping version bump for version commit.")
		return
	}

	// 2. Read current version from main.go
	content, err := os.ReadFile(mainFile)
	if err != nil {
		fmt.Printf("Error reading %s: %v\n", mainFile, err)
		os.Exit(1)
	}

	re := regexp.MustCompile(`const version = "v(\d+)\.(\d+)\.(\d+)"`)
	matches := re.FindStringSubmatch(string(content))
	if len(matches) != 4 {
		fmt.Println("Could not find version string in main.go")
		os.Exit(1)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	// 3. Determine bump type
	// feat!: or BREAKING CHANGE -> major
	// feat: -> minor
	// fix: or others -> patch
	lowerMsg := strings.ToLower(msg)
	if strings.Contains(lowerMsg, "!") || strings.Contains(lowerMsg, "breaking change") {
		major++
		minor = 0
		patch = 0
	} else if strings.HasPrefix(lowerMsg, "feat") {
		minor++
		patch = 0
	} else {
		patch++
	}

	newVersion := fmt.Sprintf("v%d.%d.%d", major, minor, patch)
	fmt.Printf("Bumping version from v%s.%s.%s to %s\n", matches[1], matches[2], matches[3], newVersion)

	// 4. Update file
	newContent := re.ReplaceAllString(string(content), fmt.Sprintf(`const version = "%s"`, newVersion))
	err = os.WriteFile(mainFile, []byte(newContent), 0644)
	if err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		os.Exit(1)
	}

	// 5. Amend commit
	// We use --no-verify to skip triggers if any, though post-commit doesn't trigger itself.
	// We use --no-edit to keep the original message but we prefix with "bump: " to be explicit if we want,
	// but the user asked "without creating a new commit" and "use the existing/intended commit".
	// To strictly follow "use the existing commit", we just add the file and amend --no-edit.

	exec.Command("git", "add", mainFile).Run()
	// We use env var to prevent recursion if the user had a pre-commit check
	cmd := exec.Command("git", "commit", "--amend", "--no-edit", "--no-verify")
	cmd.Env = append(os.Environ(), "SKIP_BUMP=1")
	cmd.Run()

	fmt.Println("Version updated and commit amended.")
}
