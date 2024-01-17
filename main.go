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

			if _, err := os.Stat(filepath.Join(repoName, ".git")); os.IsNotExist(err) {
				cmd := exec.Command("git", "init")
				cmd.Dir = repoName
				cmdOutput, err := cmd.CombinedOutput()
				if err != nil {
					log.Fatalf("git init error: %v, output: %s", err, string(cmdOutput))
				}
			} else {
				fmt.Println("Git already initialized in", repoName)
			}
		}(scanner.Text())
	}

	wg.Wait()

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading from file:", err)
	}
}
