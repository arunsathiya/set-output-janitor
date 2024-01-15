package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/google/go-github/v58/github"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}
	token := os.Getenv("GH_AUTH_TOKEN")
	if token == "" {
		log.Fatalf("Unauthorized, token empty")
	}
	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(token)
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
			repoOwner := strings.Split(fullname, "/")[0]
			repoName := strings.Split(fullname, "/")[1]
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

				types := []string{".yml", ".yaml"}
				for _, ext := range types {
					// Replace ::set-output command
					findReplaceCmd := fmt.Sprintf("find . -type f -name '*%s' -exec sed -i '' 's/echo \"::set-output name=\\(.*\\)::\\(.*\\)\"/echo \"\\1=\\2\" >> $GITHUB_OUTPUT/g' {} +", ext)
					findReplace := exec.Command("bash", "-c", findReplaceCmd)
					findReplace.Dir = repoDir
					if err := findReplace.Run(); err != nil {
						fmt.Println("Error replacing ::set-output:", err)
						return
					}

					// One more replace run
					secondFindReplaceCmd := fmt.Sprintf("find . -type f -name '*%s' -exec sed -i '' 's/echo ::set-output name=\\([^:]*\\)::\\(.*\\)/echo \"\\1=\\2\" >> \\$GITHUB_OUTPUT/g' {} +", ext)
					secondFindReplace := exec.Command("bash", "-c", secondFindReplaceCmd)
					secondFindReplace.Dir = repoDir
					if err := secondFindReplace.Run(); err != nil {
						fmt.Println("Error replacing ::set-output:", err)
						return
					}

					// Replace in single quotes
					singleQuotesFindReplaceCmd := fmt.Sprintf(`find . -type f -name '*%s' -exec sed -i '' -E "s/echo '::set-output name=([^']+)::([^']*)'/echo \"\1=\2\" >> \$GITHUB_OUTPUT/g" {} +`, ext)
					singleQuotesFindReplace := exec.Command("bash", "-c", singleQuotesFindReplaceCmd)
					singleQuotesFindReplace.Dir = repoDir
					if err := singleQuotesFindReplace.Run(); err != nil {
						fmt.Println("Error replacing ::set-output:", err)
						return
					}
				}

				// Replace in JSON files
				jsonFindReplaceCmd := `find . -type f -name '*.json' -exec sed -i '' 's/::set-output name=\([^"]*\)::\([^"]*\)/\1=\2 >> \$GITHUB_OUTPUT/g' {} +`
				jsonFindReplace := exec.Command("bash", "-c", jsonFindReplaceCmd)
				jsonFindReplace.Dir = repoDir
				if err := jsonFindReplace.Run(); err != nil {
					fmt.Println("Error replacing ::set-output:", err)
					return
				}

				// Replace in *sh files
				shFindReplaceCmd := `find . -type f -name '*.sh' -exec sed -i '' 's/echo "::set-output name=\(.*\)::\(.*\)"/echo "\1=\2" >> \$GITHUB_OUTPUT/g' {} +`
				shFindReplace := exec.Command("bash", "-c", shFindReplaceCmd)
				shFindReplace.Dir = repoDir
				if err := shFindReplace.Run(); err != nil {
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

				// Create fork
				fork, _, err := client.Repositories.CreateFork(ctx, repoOwner, repoName, &github.RepositoryCreateForkOptions{
					DefaultBranchOnly: true,
				})
				if err != nil {
					fmt.Print(err.Error())
				}
				fmt.Printf("%s", fork)

				// Get branch name
				currentBranch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
				currentBranch.Dir = repoDir
				currentBranchOutput, _ := currentBranch.Output()

				// Swap remotes: mark mine as origin and the other as upstream
				swapRemotesCmd := fmt.Sprintf("git remote rename --no-progress origin upstream && git remote add origin git@github.com:arunsathiya/%s.git", repoDir)
				swapRemotes := exec.Command("bash", "-c", swapRemotesCmd)
				swapRemotes.Dir = repoDir
				if err := swapRemotes.Run(); err != nil {
					fmt.Println("Error swapping remotes", err)
					return
				}

				// Update local main branch's tracker to origin's main, and push
				updateBranchTrackerAndPushCmd := fmt.Sprintf("git branch --unset-upstream %s && git push --set-upstream origin %s", strings.TrimSpace(string(currentBranchOutput)), strings.TrimSpace(string(currentBranchOutput)))
				updateBranchTrackerAndPush := exec.Command("bash", "-c", updateBranchTrackerAndPushCmd)
				updateBranchTrackerAndPush.Dir = repoDir
				if err := updateBranchTrackerAndPush.Run(); err != nil {
					fmt.Println("Update branch tracker and push", err)
					return
				}

				// Create PR to upstream
				prBody := "`save-state` and `set-output` commands used in GitHub Actions are deprecated and [GitHub recommends using environment files](https://github.blog/changelog/2023-07-24-github-actions-update-on-save-state-and-set-output-commands/).\n\nThis PR updates the usage of `set-output` to `$GITHUB_OUTPUT`\n\nInstructions for envvar usage from GitHub docs:\n\nhttps://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions#setting-an-output-parameter`"
				pr, _, err := client.PullRequests.Create(ctx, repoOwner, repoName, &github.NewPullRequest{
					Title:               github.String("ci: Use GITHUB_OUTPUT envvar instead of set-output command"),
					Head:                github.String(fmt.Sprintf("arunsathiya:%s", strings.TrimSpace(string(currentBranchOutput)))),
					HeadRepo:            github.String(repoName),
					Base:                github.String(strings.TrimSpace(string(currentBranchOutput))),
					Body:                github.String(prBody),
					MaintainerCanModify: github.Bool(true),
				})
				if err != nil {
					fmt.Print(err.Error())
				}
				fmt.Printf("%s", pr)
			}
		}(scanner.Text())
	}

	wg.Wait()

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading from file:", err)
	}
}
