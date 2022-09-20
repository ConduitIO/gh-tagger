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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v47/github"
	"golang.org/x/oauth2"
)

const (
	EnvGithubToken = "GITHUB_TOKEN"
)

var (
	bump    = flag.String("bump", "minor", "version bump")
	verbose = flag.Bool("verbose", false, "enable verbose output")
	org     string // github organization
	token   string // github token
)

var c *github.Client

type repo struct {
	repo      *github.Repository
	latestTag *semver.Version
	newTag    *semver.Version
}

func main() {
	parseFlags()

	ctx := context.Background()
	c = newGithubClient(ctx, token)

	repos := fetchRepos(ctx, org)
	printRepos(repos)
}

func parseFlags() {
	flag.Usage = func() {
		fmt.Fprintf(
			flag.CommandLine.Output(),
			`
This command fetches all repositories from a github organization, their latest
semantic version tag and outputs the repo name together with the bumped version.
You can control how the version is bumped using the -bump flag. This command
requires the environment variable GITHUB_TOKEN to access the github API.

You can use the output of this command as the input for gh-tag-creator to create
the new tags in all repositories.

Example:
GITHUB_TOKEN=my-token; ./gh-tag-fetcher my-org | ./gh-tag-creator

`,
		)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [github organization]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		noError(errors.New("missing argument: github organization"))
	}
	if len(args) > 1 {
		noError(fmt.Errorf("too many arguments: expected only 1 argument, got %d", len(args)))
	}

	token = os.Getenv(EnvGithubToken)
	if token == "" {
		noError(fmt.Errorf("environment variable not set: %s", EnvGithubToken))
	}

	switch *bump {
	case "patch", "minor", "major":
		// all good
	default:
		noError(fmt.Errorf("bump flag expects one of [patch minor major], got: %s", *bump))
	}

	if !*verbose {
		log.SetOutput(io.Discard) // disable all log output
	}

	org = args[0] // the only argument is the github organization
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

func fetchRepos(ctx context.Context, org string) []repo {
	log.Println("Fetching repositories (might take a minute)...")

	ghRepos, _, err := c.Repositories.ListByOrg(
		ctx,
		org,
		&github.RepositoryListByOrgOptions{},
	)
	noError(err)

	if len(ghRepos) == 0 {
		return nil
	}

	repos := make([]repo, len(ghRepos))
	for i, ghr := range ghRepos {
		r := repo{
			repo:      ghr,
			latestTag: fetchLatestTag(ctx, org, ghr.GetName()),
		}
		r.newTag = bumpVersion(r.latestTag, *bump)
		repos[i] = r
	}

	log.Println()

	return repos
}

func fetchLatestTag(ctx context.Context, org, name string) *semver.Version {
	log.Printf("Fetching latest tag for %s/%s", org, name)

	refs, _, err := c.Git.ListMatchingRefs(ctx, org, name, &github.ReferenceListOptions{
		Ref: "tags/",
	})
	noError(err)

	tags := make([]*semver.Version, 0, len(refs))
	for _, ref := range refs {
		*ref.Ref = strings.TrimPrefix(ref.GetRef(), "refs/tags/")
		v, err := semver.NewVersion(ref.GetRef())
		if err != nil {
			continue
		}

		tags = append(tags, v)
	}
	if len(tags) == 0 {
		return nil
	}

	sort.Sort(ByVersion(tags))
	return tags[len(tags)-1]
}

func bumpVersion(version *semver.Version, bump string) *semver.Version {
	if version == nil {
		version = semver.MustParse("v0.0.0")
	}
	var v semver.Version
	switch bump {
	case "major":
		v = version.IncMajor()
	case "minor":
		v = version.IncMinor()
	case "patch":
		v = version.IncPatch()
	default:
		panic(fmt.Sprintf("invalid bump: %q", bump))
	}
	return &v
}

// ByVersion implements sort.Interface for sorting semantic version strings.
type ByVersion []*semver.Version

func (vs ByVersion) Len() int      { return len(vs) }
func (vs ByVersion) Swap(i, j int) { vs[i], vs[j] = vs[j], vs[i] }
func (vs ByVersion) Less(i, j int) bool {
	return vs[i].Compare(vs[j]) < 0
}

func printRepos(repos []repo) {
	// logs will only be printed if verbose is enabled
	log.Println(strings.Join(reposToDebugOutput(repos), "\n"))
	// stdout will always be produced
	fmt.Println(strings.Join(reposToStdOutput(repos), "\n"))
}

func versionToString(v *semver.Version) string {
	if v == nil {
		return ""
	}
	return "v" + v.String()
}

func reposToDebugOutput(repos []repo) []string {
	repoLen := 0
	leftTagLen := 1
	rightTagLen := 0

	for _, r := range repos {
		if repoLen < len(r.repo.GetFullName()) {
			repoLen = len(r.repo.GetFullName())
		}
		if r.latestTag != nil && leftTagLen < len(r.latestTag.String()) {
			leftTagLen = len(r.latestTag.String()) + 1
		}
		if rightTagLen < len(r.newTag.String()) {
			rightTagLen = len(r.newTag.String()) + 1
		}
	}

	tmpl := fmt.Sprintf("github.com/%%-%ds %%%ds -> %%%ds", repoLen, leftTagLen, rightTagLen)

	out := make([]string, len(repos))
	for i, r := range repos {
		latestTag := "_"
		if r.latestTag != nil {
			latestTag = versionToString(r.latestTag)
		}
		opt := fmt.Sprintf(tmpl, r.repo.GetFullName(), latestTag, versionToString(r.newTag))
		out[i] = opt
	}
	return out
}

func reposToStdOutput(repos []repo) []string {
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = fmt.Sprintf("github.com/%s %s", r.repo.GetFullName(), versionToString(r.newTag))
	}
	return out
}
