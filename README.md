# Set Output Janitor

> **⚠️ Educational/Experimental Code Disclaimer**
> 
> This repository is shared for educational purposes and learning. The code is experimental, written for personal use, and not intended for production environments. Expect brittle implementations, hardcoded values, and quick-fix solutions throughout.

## Overview

A Go automation tool that helps migrate GitHub Actions workflows from deprecated `::set-output` commands to the recommended `$GITHUB_OUTPUT` environment variable approach.

As [announced by GitHub](https://github.blog/changelog/2022-10-11-github-actions-deprecating-save-state-and-set-output-commands/), the `save-state` and `set-output` workflow commands are deprecated and will be disabled in the future.

## What it does

This tool automates the migration process by:

1. **Forking repositories** listed in a `repos.txt` file
2. **Downloading workflow files** using GitHub's GraphQL API
3. **Performing text replacements** to convert deprecated syntax:
   - From: `::set-output name=my-output::my-value`
   - To: `my-output=my-value >> "$GITHUB_OUTPUT"`
4. **Creating commits** with the changes using git patches
5. **Opening pull requests** with standardized descriptions

## Setup

### Prerequisites

- Go 1.21.5 or later
- GitHub personal access token with repository permissions

### Configuration

1. Create a `.env` file with your GitHub token:
   ```
   GITHUB_TOKEN=your_github_token_here
   ```

2. Create a `repos.txt` file listing repositories and workflow files to process:
   ```
   owner/repo/.github/workflows/workflow.yml
   owner/repo/.github/workflows/another.yml
   ```

### Dependencies

Install dependencies:
```bash
go mod download
```

## Usage

Run the tool:
```bash
go run main.go
```

The tool will:
- Fork the listed repositories to your GitHub account
- Download and process the specified workflow files
- Create commits with the `set-output` fixes
- Open pull requests against the original repositories

## Key Dependencies

- **GitHub GraphQL API** (`github.com/shurcooL/githubv4`) - For file operations
- **GitHub REST API** (`github.com/google/go-github/v58`) - For repository management
- **Patch2PR** (`github.com/bluekeyes/patch2pr`) - For applying git patches via GraphQL
- **Go-GitDiff** (`github.com/bluekeyes/go-gitdiff`) - For parsing git diffs

## Technical Notes

- Uses concurrent goroutines for processing multiple repositories
- Relies on shell commands (`sed`, `grep`, `git`) for text processing
- Creates local git repositories for each processed project
- Hardcoded commit author and PR template information

## Learning Context

This tool was built while learning Go and experimenting with GitHub's APIs. It demonstrates:
- GitHub GraphQL and REST API integration
- Concurrent programming with goroutines and channels
- Git patch operations
- File system operations and shell command execution