package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/bluekeyes/patch2pr"
	"github.com/google/go-github/v58/github"
	"github.com/joho/godotenv"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatalf("Unauthorized, token empty")
	}
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	httpClient := oauth2.NewClient(context.Background(), src)
	v4client := githubv4.NewClient(httpClient)
	_ = github.NewClient(nil).WithAuthToken(token)
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

				// Update local main branch's tracker to origin's main
				updateBranchTrackerCmd := fmt.Sprintf("git branch --unset-upstream %s", strings.TrimSpace(string(currentBranchOutput)))
				updateBranchTracker := exec.Command("bash", "-c", updateBranchTrackerCmd)
				updateBranchTracker.Dir = repoDir
				if err := updateBranchTracker.Run(); err != nil {
					fmt.Println("Update branch tracker", err)
					return
				}

				generatePatchCmd := "git diff > changes.patch"
				generatePatch := exec.Command("bash", "-c", generatePatchCmd)
				generatePatch.Dir = repoDir
				if err := generatePatch.Run(); err != nil {
					fmt.Println("Error generating patch", err)
					return
				}

				patch, err := os.Open(filepath.Join(repoDir, "changes.patch"))
				if err != nil {
					log.Fatalf(err.Error())
				}
				files, _, err := gitdiff.Parse(patch)
				if err != nil {
					log.Fatal(err)
				}
				var query struct {
					Repository struct {
						ID githubv4.ID
					} `graphql:"repository(owner: \"replit\", name: \"pyright-extended\")"`
				}
				err = v4client.Query(context.Background(), &query, nil)
				if err != nil {
					fmt.Println(err)
					return
				}
				fmt.Println(query.Repository.ID)
				graphqlApplier := patch2pr.NewGraphQLApplier(
					v4client,
					patch2pr.Repository{
						Owner: "arunsathiya",
						Name:  repoName,
					},
					strings.TrimSpace(string(currentBranchOutput)),
				)
				for _, file := range files {
					graphqlApplier.Apply(context.Background(), file)
				}
			}
		}(scanner.Text())
	}

	wg.Wait()

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading from file:", err)
	}
}
