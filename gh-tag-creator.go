// Copyright © 2022 Meroxa, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/google/go-github/v47/github"
	"golang.org/x/oauth2"
)

const (
	EnvGithubToken = "GITHUB_TOKEN"
)

var (
	verbose = flag.Bool("verbose", false, "enable verbose output")
	yes     = flag.Bool("yes", false, "don't ask for confirmation")
	token   string // github token
)

var c *github.Client

type repo struct {
	repo *github.Repository
	tag  string
}

func main() {
	parseFlags()

	input := make([]string, 0)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		input = append(input, scanner.Text())
	}
	noError(scanner.Err())

	if len(input) == 0 {
		noError(errors.New("no input"))
	}

	ctx := context.Background()
	c = newGithubClient(ctx, token)

	repos := fetchRepos(ctx, input)
	if !*yes {
		fmt.Println()
		fmt.Println(strings.Join(reposToStdOutput(repos), "\n"))
		fmt.Println()
		fmt.Print("Do you want to create the tags listed above? [Y/N]: ")

		y := make([]byte, 1)
		in, err := os.Open("/dev/tty") // stdin is already used to get repo list, we read directly from the terminal
		noError(err)
		defer in.Close()
		_, err = in.Read(y)
		noError(err)

		if y[0] != 'y' && y[0] != 'Y' {
			noError(errors.New("abort"))
		}
	}

	createTags(ctx, repos)
}

func parseFlags() {
	flag.Usage = func() {
		fmt.Fprintf(
			flag.CommandLine.Output(),
			`
This command creates tags in multiple repositories. The tags are created on the
latest commit on the default branch. This command requires the environment
variable GITHUB_TOKEN to access the github API.

The command expects a list of repositories and tags passed to stdin, use another
command to pipe the list to the command. You can use gh-tag-fetcher to create
the list.

Example:
GITHUB_TOKEN=my-token; ./gh-tag-fetcher my-org | ./gh-tag-creator
or
cat repo-list.txt | ./gh-tag-creator

`,
		)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	token = os.Getenv(EnvGithubToken)
	if token == "" {
		noError(fmt.Errorf("environment variable not set: %s", EnvGithubToken))
	}

	if !*verbose {
		log.SetOutput(io.Discard) // disable all log output
	}
}

func noError(err error) {
	if err != nil {
		log.SetOutput(os.Stderr) // in case we disabled log output
		log.Fatalf("Error: %+v", err)
	}
}

func newGithubClient(ctx context.Context, token string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

func fetchRepos(ctx context.Context, input []string) []repo {
	repos := make([]repo, len(input))
	for i, line := range input {
		tokens := strings.Split(line, " ")
		if len(tokens) < 2 || len(tokens) > 2 {
			noError(fmt.Errorf("error parsing input line: expected 2 tokens separated with a space: %s", line))
		}
		repoURL, tag := tokens[0], tokens[1]

		repoURL = strings.TrimPrefix(repoURL, "github.com/")
		tokens = strings.Split(repoURL, "/")
		if len(tokens) < 2 || len(tokens) > 2 {
			noError(fmt.Errorf("error parsing input line: invalid repo URL: %s", line))
		}

		org := tokens[0]
		name := tokens[1]
		log.Printf("Fetching info for %s/%s", org, name)
		r, _, err := c.Repositories.Get(ctx, org, name)
		noError(err)

		repos[i] = repo{
			repo: r,
			tag:  tag,
		}
	}
	return repos
}

func reposToStdOutput(repos []repo) []string {
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = fmt.Sprintf("github.com/%s %s", r.repo.GetFullName(), r.tag)
	}
	return out
}

func createTags(ctx context.Context, repos []repo) {
	for _, r := range repos {
		org := r.repo.GetOwner().GetLogin()
		name := r.repo.GetName()

		branch := r.repo.GetDefaultBranch()
		commit, _, err := c.Repositories.GetCommit(ctx, org, name, branch, &github.ListOptions{})
		noError(err)

		log.Printf("Creating tag %s in %s (SHA: %s)", r.tag, r.repo.GetFullName(), commit.GetSHA())

		createdRef, _, err := c.Git.CreateRef(ctx, org, name, &github.Reference{
			Ref: github.String("refs/tags/" + r.tag),
			Object: &github.GitObject{
				SHA: commit.SHA,
			},
		})
		noError(err)

		log.Printf("Created ref: %s", createdRef.String())
	}
}
