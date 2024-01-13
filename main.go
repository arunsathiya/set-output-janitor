package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

func main() {
	// Open the file containing the list of repositories
	file, err := os.Open("repos.txt")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	var wg sync.WaitGroup
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		wg.Add(1)
		go func(line string) {
			defer wg.Done()
			fullname := strings.Split(line, " ")[3]
			repoDir := strings.Split(fullname, "/")[1]
			gitClone := exec.Command("gh", "repo", "clone", fullname, repoDir)
			if err := gitClone.Run(); err != nil {
				fmt.Println("Error cloning repository:", err)
				return
			}

			// Check for existing PRs
			prListCmd := "gh pr list --author \"@me\""
			prList := exec.Command("bash", "-c", prListCmd)
			prList.Dir = repoDir
			prListOutput, err := prList.Output()
			if err != nil {
				fmt.Println("Error in prList check:", err)
				return
			}
			if len(prListOutput) == 0 {
				// Check for ::set-output in cloned directory
				grepCmd := "grep -rnw '.' -e '::set-output'"
				grep := exec.Command("bash", "-c", grepCmd)
				grep.Dir = repoDir
				if err := grep.Run(); err != nil {
					fmt.Println("::set-output not found or error in grep:", err)
					return
				}

				// Replace ::set-output command
				findReplaceCmd := "find . -type f -name '*.yml' -exec sed -i '' 's/echo \"::set-output name=\\(.*\\)::\\(.*\\)\"/echo \"\\1=\\2\" >> $GITHUB_OUTPUT/g' {} +"
				findReplace := exec.Command("bash", "-c", findReplaceCmd)
				findReplace.Dir = repoDir
				if err := findReplace.Run(); err != nil {
					fmt.Println("Error replacing ::set-output:", err)
					return
				}

				// One more replace run
				secondFindReplaceCmd := "find . -type f -name '*.yml' -exec sed -i '' 's/echo ::set-output name=\\([^:]*\\)::\\(.*\\)/echo \"\\1=\\2\" >> \\$GITHUB_OUTPUT/g' {} +"
				secondFindReplace := exec.Command("bash", "-c", secondFindReplaceCmd)
				secondFindReplace.Dir = repoDir
				if err := secondFindReplace.Run(); err != nil {
					fmt.Println("Error replacing ::set-output:", err)
					return
				}

				// Commit changes
				commitCmd := "git add . && git commit -m \"ci: Use GITHUB_OUTPUT envvar instead of set-output command\""
				commit := exec.Command("bash", "-c", commitCmd)
				commit.Dir = repoDir
				if commit.Run(); err != nil {
					fmt.Println("Error committing changes:", err)
					return
				}

				// Check for ::set-output once more
				grepOnceMoreCmd := "grep -rnw '.' -e '::set-output'"
				grepOnceMore := exec.Command("bash", "-c", grepOnceMoreCmd)
				grepOnceMore.Dir = repoDir
				grepOnceMoreOutput, _ := grepOnceMore.Output()
				if len(grepOnceMoreOutput) > 0 {
					fmt.Printf("::set-output found in %s:\n%s\n", repoDir, grepOnceMoreOutput)
				}

				// Check for ::save-state in cloned directory
				grepSaveStateCmd := "grep -rnw '.' -e '::save-state'"
				grepSaveState := exec.Command("bash", "-c", grepSaveStateCmd)
				grepSaveState.Dir = repoDir
				grepSaveStateOutput, _ := grepSaveState.Output()
				if len(grepSaveStateOutput) > 0 {
					fmt.Printf("::save-state found in %s:\n%s\n", repoDir, grepSaveStateOutput)
				}
			}
		}(scanner.Text())
	}

	wg.Wait()

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading from file:", err)
	}
}
