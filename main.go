package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/joho/godotenv"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

type FileContentQuery struct {
	Repository struct {
		Object struct {
			Blob struct {
				Text githubv4.String
			} `graphql:"... on Blob"`
		} `graphql:"object(expression: $expression)"`
	} `graphql:"repository(name: $name, owner: $owner)"`
}

type FileContentResponse struct {
	Data struct {
		Repository struct {
			Object struct {
				Blob struct {
					Text string `json:"text"`
				} `json:"blob"`
			} `json:"object"`
		} `json:"repository"`
	} `json:"data"`
}

func fetchFileContent(client *githubv4.Client, owner, name, expression string) (string, error) {
	var query FileContentQuery
	variables := map[string]interface{}{
		"owner":      githubv4.String(owner),
		"name":       githubv4.String(name),
		"expression": githubv4.String(expression),
	}

	err := client.Query(context.Background(), &query, variables)
	if err != nil {
		return "", err
	}

	return string(query.Repository.Object.Blob.Text), nil
}

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
	client := githubv4.NewClient(httpClient)
	// Open the file containing the list of repositories
	file, err := os.Open("repos.txt")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	var initializedRepos = make(map[string]bool)
	var mu sync.Mutex

	var wg sync.WaitGroup
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		wg.Add(1)
		go func(line string) {
			defer wg.Done()
			parts := strings.Split(line, "/")
			repoOwner := parts[0]
			repoName := parts[1]
			filePath := strings.Join(parts[2:], "/")
			expression := fmt.Sprintf("HEAD:%s", filePath)

			repoKey := fmt.Sprintf("%s/%s", repoOwner, repoName)
			mu.Lock()

			if _, exists := initializedRepos[repoKey]; !exists {
				initializedRepos[repoKey] = true
				mu.Unlock()

				// Create directories
				dir := filepath.Join(repoName, ".github", "workflows")
				if _, err := os.Stat(dir); os.IsNotExist(err) {
					os.MkdirAll(dir, os.ModePerm)
				}

				// Create file
				fullPath := filepath.Join(repoName, filePath)
				if _, err := os.Stat(fullPath); os.IsNotExist(err) {
					file, err := os.Create(fullPath)
					if err != nil {
						log.Fatal(err)
					}
					file.Close()
				}

				fileContent, err := fetchFileContent(client, repoOwner, repoName, expression)
				if err != nil {
					fmt.Println("Error fetching file content:", err)
					return
				}

				// Write the content to a file
				err = os.WriteFile(path.Join(repoName, filePath), []byte(fileContent), 0644)
				if err != nil {
					fmt.Println("Error writing file:", err)
				}

				// Git initialization and setup remote
				if _, err := os.Stat(filepath.Join(repoName, ".git")); os.IsNotExist(err) {
					fCmd := fmt.Sprintf("git init && git remote add origin git@github.com:%s/%s.git", repoOwner, repoName)
					cmd := exec.Command("sh", "-c", fCmd)
					cmd.Dir = repoName
					cmdOutput, err := cmd.CombinedOutput()
					if err != nil {
						log.Fatalf("git init error: %v, output: %s", err, string(cmdOutput))
					}
				} else {
					fmt.Println("Git already initialized in", repoName)
				}

				// Initial commit
				fCmd := "git add . && git commit -m \"taken from source\""
				cmd := exec.Command("sh", "-c", fCmd)
				cmd.Dir = repoName
				if err := cmd.Run(); err != nil {
					log.Fatal(err)
				}

				// Replace output
				if err := processReplacements(repoName); err != nil {
					log.Fatal(err)
				}

				// Generate patch
				if err := genPatch(repoName); err != nil {
					log.Fatal(err)
				}
			} else {
				mu.Unlock()
				return
			}
		}(scanner.Text())
	}

	wg.Wait()

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading from file:", err)
	}
}

func processReplacements(repoDir string) error {
	// Check for ::set-output in cloned directory
	grepCmd := "grep -rnw '.' -e '::set-output'"
	grep := exec.Command("bash", "-c", grepCmd)
	grep.Dir = repoDir
	if err := grep.Run(); err != nil {
		return fmt.Errorf("::set-output not found or error in grep: %s", err)
	}

	types := []string{".yml", ".yaml"}
	for _, ext := range types {
		// Replace ::set-output command
		findReplaceCmd := fmt.Sprintf("find . -type f -name '*%s' -exec sed -i '' 's/echo \"::set-output name=\\(.*\\)::\\(.*\\)\"/echo \"\\1=\\2\" >> $GITHUB_OUTPUT/g' {} +", ext)
		findReplace := exec.Command("bash", "-c", findReplaceCmd)
		findReplace.Dir = repoDir
		if err := findReplace.Run(); err != nil {
			return fmt.Errorf("error replacing ::set-output: %s", err)
		}

		// One more replace run
		secondFindReplaceCmd := fmt.Sprintf("find . -type f -name '*%s' -exec sed -i '' 's/echo ::set-output name=\\([^:]*\\)::\\(.*\\)/echo \"\\1=\\2\" >> \\$GITHUB_OUTPUT/g' {} +", ext)
		secondFindReplace := exec.Command("bash", "-c", secondFindReplaceCmd)
		secondFindReplace.Dir = repoDir
		if err := secondFindReplace.Run(); err != nil {
			return fmt.Errorf("error replacing ::set-output: %s", err)
		}

		// Replace in single quotes
		singleQuotesFindReplaceCmd := fmt.Sprintf(`find . -type f -name '*%s' -exec sed -i '' -E "s/echo '::set-output name=([^']+)::([^']*)'/echo \"\1=\2\" >> \$GITHUB_OUTPUT/g" {} +`, ext)
		singleQuotesFindReplace := exec.Command("bash", "-c", singleQuotesFindReplaceCmd)
		singleQuotesFindReplace.Dir = repoDir
		if err := singleQuotesFindReplace.Run(); err != nil {
			return fmt.Errorf("error replacing ::set-output: %s", err)
		}
	}

	// Replace in JSON files
	jsonFindReplaceCmd := `find . -type f -name '*.json' -exec sed -i '' 's/::set-output name=\([^"]*\)::\([^"]*\)/\1=\2 >> \$GITHUB_OUTPUT/g' {} +`
	jsonFindReplace := exec.Command("bash", "-c", jsonFindReplaceCmd)
	jsonFindReplace.Dir = repoDir
	if err := jsonFindReplace.Run(); err != nil {
		return fmt.Errorf("error replacing ::set-output: %s", err)
	}

	// Replace in *sh files
	shFindReplaceCmd := `find . -type f -name '*.sh' -exec sed -i '' 's/echo "::set-output name=\(.*\)::\(.*\)"/echo "\1=\2" >> \$GITHUB_OUTPUT/g' {} +`
	shFindReplace := exec.Command("bash", "-c", shFindReplaceCmd)
	shFindReplace.Dir = repoDir
	if err := shFindReplace.Run(); err != nil {
		return fmt.Errorf("error replacing ::set-output: %s", err)
	}
	return nil
}

func genPatch(repoName string) error {
	fCmd := "git diff > changes.patch && git reset --hard"
	cmd := exec.Command("sh", "-c", fCmd)
	cmd.Dir = repoName
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("patch creation failed: %s", err)
	}
	return nil
}
