package main

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/bluekeyes/patch2pr"
	"github.com/google/go-github/v58/github"
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

type OidQuery struct {
	Repository struct {
		DefaultBranchRef struct {
			Target struct {
				OID githubv4.String
			} `graphql:"target"`
		} `graphql:"defaultBranchRef"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

type OidResponse struct {
	Data struct {
		Repository struct {
			DefaultBranchRef struct {
				Target struct {
					OID string `json:"oid"`
				} `json:"target"`
			} `json:"defaultBranchRef"`
		} `json:"repository"`
	} `json:"data"`
}

func fetchOid(client *githubv4.Client, owner, name string) (string, error) {
	var query OidQuery
	variables := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}

	err := client.Query(context.Background(), &query, variables)
	if err != nil {
		return "", err
	}

	return string(query.Repository.DefaultBranchRef.Target.OID), nil
}

type customError struct {
	errType string
	message error
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
	clientv3 := github.NewClient(nil).WithAuthToken(token)
	// Open the file containing the list of repositories
	file, err := os.Open("repos.txt")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	var wg sync.WaitGroup
	scanner := bufio.NewScanner(file)

	scannedLines := []string{}
	for scanner.Scan() {
		scannedLines = append(scannedLines, scanner.Text())
	}

	errChan := make(chan error, len(scannedLines))
	for _, scannedLine := range scannedLines {
		wg.Add(1)
		go func(line string) {
			defer wg.Done()
			parts := strings.Split(line, "/")
			repoOwner := parts[0]
			repoName := parts[1]

			fork, _, _ := clientv3.Repositories.Get(context.Background(), "arunsathiya", repoName)
			if fork == nil {
				_, _, err = clientv3.Repositories.CreateFork(context.Background(), repoOwner, repoName, &github.RepositoryCreateForkOptions{
					DefaultBranchOnly: true,
				})
				if err != nil {
					errChan <- err
					return
				}
			}

			time.Sleep(2 * time.Second)

			// Create directories
			dir := filepath.Join(repoName, ".github", "workflows")
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				os.MkdirAll(dir, os.ModePerm)
			}
		}(scannedLine)
	}
	go func() {
		wg.Wait()
		close(errChan)
	}()
	for err := range errChan {
		if err != nil {
			log.Printf("error: %v", err)
		}
	}

	repoOwner := strings.Split(scannedLines[0], "/")[0]
	scannedDirs := []fs.DirEntry{}
	dirs, err := os.ReadDir(".")
	if err != nil {
		log.Fatalf("error reading the root directory: %v", err)
	}
	scannedDirs = append(scannedDirs, dirs...)
	gitErrChan := make(chan error, len(scannedDirs))
	for _, scannedDir := range scannedDirs {
		if scannedDir.IsDir() && scannedDir.Name() != ".git" {
			wg.Add(1)
			go func(scannedDir fs.DirEntry) {
				defer wg.Done()
				repoName := scannedDir.Name()
				if _, err := os.Stat(filepath.Join(repoName, ".git")); os.IsNotExist(err) {
					fCmd := fmt.Sprintf("git init && git remote add origin git@github.com:%s/%s.git", repoOwner, repoName)
					cmd := exec.Command("sh", "-c", fCmd)
					cmd.Dir = repoName
					_, err := cmd.CombinedOutput()
					if err != nil {
						gitErrChan <- err
						return
					}
				}
			}(scannedDir)
		}
	}
	go func() {
		wg.Wait()
		close(gitErrChan)
	}()
	for err := range gitErrChan {
		if err != nil {
			log.Printf("git init error: %v", err)
		}
	}

	workflowFileErrChan := make(chan customError, len(scannedLines))
	for _, scannedLine := range scannedLines {
		wg.Add(1)
		go func(scannedLine string) {
			defer wg.Done()
			parts := strings.Split(scannedLine, "/")
			repoOwner := parts[0]
			repoName := parts[1]
			filePath := strings.Join(parts[2:], "/")
			expression := fmt.Sprintf("HEAD:%s", filePath)

			// Create file
			fullPath := filepath.Join(repoName, filePath)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				file, err := os.Create(fullPath)
				if err != nil {
					workflowFileErrChan <- customError{"create", err}
					return
				}
				file.Close()
			}

			fileContent, err := fetchFileContent(client, repoOwner, repoName, expression)
			if err != nil {
				workflowFileErrChan <- customError{"read", err}
				return
			}

			// Write the content to a file
			err = os.WriteFile(path.Join(repoName, filePath), []byte(fileContent), 0644)
			if err != nil {
				workflowFileErrChan <- customError{"write", err}
			}
		}(scannedLine)
	}
	go func() {
		wg.Wait()
		close(workflowFileErrChan)
	}()
	for err := range workflowFileErrChan {
		if err.message != nil {
			switch err.errType {
			case "create":
				log.Printf("file creation error: %v", err.message)
			case "fetch":
				log.Printf("file contents fetch error: %v", err.message)
			case "write":
				log.Printf("write contents error: %v", err.message)
			}
		}
	}

	commitErrChan := make(chan error, len(scannedDirs))
	for _, scannedDir := range scannedDirs {
		if scannedDir.IsDir() && scannedDir.Name() != ".git" {
			wg.Add(1)
			go func(scannedDir fs.DirEntry) {
				defer wg.Done()
				repoName := scannedDir.Name()
				fCmd := "git add . && git commit -m \"taken from source\""
				cmd := exec.Command("sh", "-c", fCmd)
				cmd.Dir = repoName
				if _, err := cmd.CombinedOutput(); err != nil {
					gitErrChan <- err
					return
				}
			}(scannedDir)
		}
	}
	go func() {
		wg.Wait()
		close(commitErrChan)
	}()
	for err := range commitErrChan {
		if err != nil {
			log.Printf("error creating initial commit: %v", err)
		}
	}

	commitPatchErrChan := make(chan customError, len(scannedDirs))
	for _, scannedDir := range scannedDirs {
		if scannedDir.IsDir() && scannedDir.Name() != ".git" {
			wg.Add(1)
			go func(scannedDir fs.DirEntry) {
				defer wg.Done()
				repoName := scannedDir.Name()
				if err := processReplacements(repoName); err != nil {
					commitPatchErrChan <- customError{"replacements", err}
					return
				}
				if err := genPatch(repoName); err != nil {
					commitPatchErrChan <- customError{"generating patch", err}
					return
				}
				patch, err := os.Open(filepath.Join(repoName, "changes.patch"))
				if err != nil {
					commitPatchErrChan <- customError{"open patch", err}
					return
				}
				patchFiles, _, err := gitdiff.Parse(patch)
				if err != nil {
					commitPatchErrChan <- customError{"parse patch", err}
					return
				}
				if len(patchFiles) == 0 {
					commitPatchErrChan <- customError{"patch files length", fmt.Errorf("patch is empty for %s", repoName)}
					return
				}
				fork, _, _ := clientv3.Repositories.Get(context.Background(), "arunsathiya", repoName)
				oid, err := fetchOid(client, *fork.Owner.Login, *fork.Name)
				if err != nil {
					commitPatchErrChan <- customError{"oid", err}
					return
				}
				graphqlApplier := patch2pr.NewGraphQLApplier(
					client,
					patch2pr.Repository{
						Owner: *fork.Owner.Login,
						Name:  *fork.Name,
					},
					oid,
				)
				for _, patchFile := range patchFiles {
					err := graphqlApplier.Apply(context.Background(), patchFile)
					if err != nil {
						if patch2pr.IsUnsupported(err) {
							commitPatchErrChan <- customError{"unsupported apply operation", err}
							return
						} else {
							commitPatchErrChan <- customError{"error applying", err}
							return
						}
					}
				}
				prTitle := "ci: Use GITHUB_OUTPUT envvar instead of set-output command"
				prBody := "`save-state` and `set-output` commands used in GitHub Actions are deprecated and [GitHub recommends using environment files](https://github.blog/changelog/2023-07-24-github-actions-update-on-save-state-and-set-output-commands/).\n\nThis PR updates the usage of `::set-output` to `\"$GITHUB_OUTPUT\"`\n\nInstructions for envvar usage from GitHub docs:\n\nhttps://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions#setting-an-output-parameter"
				sha, err := graphqlApplier.Commit(
					context.Background(),
					"refs/heads/"+*fork.DefaultBranch,
					&gitdiff.PatchHeader{
						Author: &gitdiff.PatchIdentity{
							Name:  "Arun",
							Email: "arun@arun.blog",
						},
						AuthorDate: time.Now(),
						Committer: &gitdiff.PatchIdentity{
							Name:  "Arun",
							Email: "arun@arun.blog",
						},
						CommitterDate: time.Now(),
						Title:         prTitle,
						Body:          prBody,
					},
				)
				fmt.Printf("Commit SHA for %s: %s\n", repoName, sha)
				if err != nil {
					commitPatchErrChan <- customError{"error preparing commit", err}
					return
				}
				time.Sleep(2 * time.Second)
				var maintainerCanModify bool = true
				var draft bool = false
				var base string = *fork.Source.DefaultBranch
				var head string = *fork.Owner.Login + ":" + *fork.DefaultBranch
				var headRepo string = *fork.Name
				_, _, err = clientv3.PullRequests.Create(context.Background(), repoOwner, repoName, &github.NewPullRequest{
					Title:               &prTitle,
					Body:                &prBody,
					MaintainerCanModify: &maintainerCanModify,
					Draft:               &draft,
					Base:                &base,
					Head:                &head,
					HeadRepo:            &headRepo,
				})
				if err != nil {
					commitPatchErrChan <- customError{"error preparing PR", err}
					return
				}
			}(scannedDir)
		}
	}
	go func() {
		wg.Wait()
		close(commitPatchErrChan)
	}()
	for err := range commitPatchErrChan {
		if err.message != nil {
			switch err.errType {
			case "replacements":
				log.Printf("error processing replacements: %v", err.message)
			case "generating patch":
				log.Printf("error generating patch: %v", err.message)
			case "open patch":
				log.Printf("error opening patch: %v", err.message)
			case "parse patch":
				log.Printf("error parsing patch: %v", err.message)
			case "patch files length":
				log.Printf("error with patch files length: %v", err.message)
			case "unsupported apply operation":
				log.Printf("error in applying operation: %v", err.message)
			case "error applying":
				log.Printf("error applying: %v", err.message)
			case "error preparing commit":
				log.Printf("error preparing commit: %v", err.message)
			case "error preparing PR":
				log.Printf("error preparing PR: %v", err.message)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading from file:", err)
	}
}

func processReplacements(repoDir string) error {
	// Check for ::set-output in cloned directory
	grepCmd := "grep -rnw '.' -e 'set-output'"
	grep := exec.Command("bash", "-c", grepCmd)
	grep.Dir = repoDir
	if err := grep.Run(); err != nil {
		return fmt.Errorf("set-output not found or error in grep: %s", err)
	}
	types := []string{".yml", ".yaml"}
	for _, ext := range types {
		// Replace ::set-output command
		fCmd := fmt.Sprintf(`LC_ALL=C find . -type f -name '*%s' -exec sed -i '' -E 's/::set-output name=([^:]*)::(.*)/\1=\2 >> "$GITHUB_OUTPUT"/g' {} +`, ext)
		cmd := exec.Command("bash", "-c", fCmd)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("error replacing: %v", err)
		}
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
