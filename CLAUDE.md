# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is a personal automation tool written in Go that fixes deprecated `set-output` commands in GitHub Actions workflows. The tool:
- Forks repositories from a list
- Downloads workflow files using GitHub GraphQL API
- Replaces `::set-output` commands with `$GITHUB_OUTPUT` environment variable usage
- Creates commits and pull requests with the fixes

## Architecture

The application is a single Go binary (`main.go`) that orchestrates the entire workflow:

1. **Repository Management**: Forks repositories listed in `repos.txt` (not included in repo)
2. **File Fetching**: Uses GitHub GraphQL API to download workflow files
3. **Text Processing**: Uses `sed` commands to replace deprecated syntax
4. **Git Operations**: Creates commits and applies patches using `patch2pr` library
5. **PR Creation**: Automatically creates pull requests with standardized titles/bodies

Key components:
- `fetchFileContent()`: GraphQL queries for file contents from repositories
- `fetchOid()`: Gets commit SHA for patch operations  
- `processReplacements()`: Performs `sed` regex replacements on workflow files
- `genPatch()`: Creates git patches from changes

## Development Commands

### Build and Run
```bash
go build                    # Build the binary
go run main.go             # Run directly
```

### Testing and Quality
```bash
go test ./...              # Run tests (if any)
go vet                     # Static analysis
go fmt                     # Format code
```

### Dependencies
```bash
go mod tidy               # Clean up dependencies
go mod download           # Download dependencies
```

## Configuration

- Requires `GITHUB_TOKEN` environment variable (loaded from `.env` file)
- Expects `repos.txt` file with repository paths in format `owner/repo/path/to/workflow.yml`
- Creates local directories matching repository names for git operations

## Key Dependencies

- `github.com/shurcooL/githubv4`: GitHub GraphQL API client
- `github.com/google/go-github/v58`: GitHub REST API client
- `github.com/bluekeyes/patch2pr`: Git patch operations via GraphQL
- `github.com/joho/godotenv`: Environment variable loading

## Notes

- This is experimental/personal-use code with "brittle" implementation as noted in README
- Heavy use of goroutines and error channels for concurrent operations
- Hardcoded commit author information and PR templates
- Uses shell commands (`sed`, `grep`, `git`) for file processing