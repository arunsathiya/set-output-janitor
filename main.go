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

			// Check for ::set-output in cloned directory
			fmt.Println("Checking for ::set-output in", repoDir)
			grepCmd := "grep -rnw '" + repoDir + "' -e '::set-output'"
			grep := exec.Command("bash", "-c", grepCmd)
			output, err := grep.Output()
			if err != nil {
				fmt.Println("::set-output not found or error in grep:", err)
				return
			}
			fmt.Printf("::set-output found in %s:\n%s\n", repoDir, output)

			// Replace ::set-output command
			fmt.Println("Replacing ::set-output in", repoDir)
			findCmd := "find . -type f -name '*.yml' -exec sed -i '' 's/echo \"::set-output name=\\(.*\\)::\\(.*\\)\"/echo \"\\1=\\2\" >> $GITHUB_OUTPUT/g' {} +"
			find := exec.Command("bash", "-c", findCmd)
			find.Dir = repoDir
			output, err = find.Output()
			if err != nil {
				fmt.Println("Error replacing ::set-output:", err)
				return
			}
		}(scanner.Text())
	}

	wg.Wait()

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading from file:", err)
	}
}
