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
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v47/github"
	"github.com/pterm/pterm"
	"golang.org/x/oauth2"
)

const (
	EnvGithubToken = "GITHUB_TOKEN"
	DoIt           = "DO IT"

	org  = "conduitio" // TODO get from flag
	bump = "minor"
)

var c *github.Client

type repo struct {
	repo      *github.Repository
	latestTag *semver.Version
	newTag    *semver.Version
}

func main() {
	token := os.Getenv(EnvGithubToken)
	if token == "" {
		log.Fatalf("Environemnt variable %v is not set", EnvGithubToken)
	}

	ctx := context.Background()
	c = newGithubClient(ctx, token)

	repos := fetchRepos(ctx, org)
	repos = filter(repos)
	if len(repos) == 0 {
		pterm.Warning.Println("No repositories selected, abort!")
		os.Exit(1)
	}

	confirm()
	createTags(ctx, repos)
}

func noError(err error) {
	if err != nil {
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
	pterm.Info.Printfln("Fetching repositories (might take a minute)...")

	ghRepos, _, err := c.Repositories.ListByOrg(
		ctx,
		org,
		&github.RepositoryListByOrgOptions{},
	)
	noError(err)

	if len(ghRepos) == 0 {
		return nil
	}

	// Create progressbar as fork from the default progressbar.
	p, err := pterm.DefaultProgressbar.
		WithTotal(len(ghRepos)).
		WithTitle("Fetching repo tags").
		WithRemoveWhenDone(true).
		Start()
	noError(err)

	repos := make([]repo, len(ghRepos))
	for i, ghr := range ghRepos {
		p.UpdateTitle("Fetching " + ghr.GetName()) // Update the title of the progressbar.

		r := repo{
			repo:      ghr,
			latestTag: fetchLatestTag(ctx, org, ghr.GetName()),
		}
		r.newTag = bumpVersion(r.latestTag, bump)
		repos[i] = r

		pterm.Success.Println("Fetching " + ghr.GetName()) // If a progressbar is running, each print will be printed above the progressbar.
		p.Increment()
	}

	_, err = p.Stop()
	noError(err)
	pterm.Println()

	return repos
}

func fetchLatestTag(ctx context.Context, org, name string) *semver.Version {
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

func filter(repos []repo) []repo {
	opts := buildOptions(repos)

	selectedOptions, err := pterm.DefaultInteractiveMultiselect.
		WithMaxHeight(len(opts)).
		WithOptions(opts).
		WithDefaultOptions(opts).
		Show("Please select repositories that should be tagged")
	noError(err)

	filtered := make([]repo, 0, len(repos))
	for i, v1 := range opts {
		for _, v2 := range selectedOptions {
			if v1 == v2 {
				filtered = append(filtered, repos[i])
			}
		}
	}

	return filtered
}

func buildOptions(repos []repo) []string {
	repoLen := 0
	leftTagLen := 1
	rightTagLen := 0

	for _, r := range repos {
		if repoLen < len(r.repo.GetName()) {
			repoLen = len(r.repo.GetName())
		}
		if r.latestTag != nil && leftTagLen < len(r.latestTag.String()) {
			leftTagLen = len(r.latestTag.String()) + 1
		}
		if rightTagLen < len(r.newTag.String()) {
			rightTagLen = len(r.newTag.String()) + 1
		}
	}

	emptyTag := strings.Repeat("_", leftTagLen)
	tmpl := fmt.Sprintf("%%-%ds (%%%ds -> %%%ds)", repoLen, leftTagLen, rightTagLen)

	opts := make([]string, len(repos))
	for i, r := range repos {
		latestTag := emptyTag
		if r.latestTag != nil {
			latestTag = "v" + r.latestTag.String()
		}
		newTag := "v" + r.newTag.String()
		opt := fmt.Sprintf(tmpl, r.repo.GetName(), latestTag, newTag)
		opts[i] = opt
	}
	return opts
}

func confirm() {
	text, err := pterm.DefaultInteractiveTextInput.Show("Please confirm that you want to apply the tags above by writing \"" + pterm.Green(DoIt) + "\" (" + pterm.Red("this action CAN NOT be reversed!") + ")")
	noError(err)

	if text != DoIt {
		pterm.Warning.Println("Abort!")
		os.Exit(1)
	}
}

func createTags(ctx context.Context, repos []repo) {
	for _, r := range repos {
		pterm.Println()

		branch := r.repo.GetDefaultBranch()
		commit, _, err := c.Repositories.GetCommit(ctx, org, r.repo.GetName(), branch, &github.ListOptions{})
		noError(err)

		confirm, err := pterm.DefaultInteractiveConfirm.Show(fmt.Sprintf("Confirm you want to create tag v%s on branch %v (SHA: %s)", r.newTag.String(), branch, commit.GetSHA()))
		noError(err)
		if !confirm {
			pterm.Warning.Println("Skip")
		}

		createdRef, _, err := c.Git.CreateRef(ctx, org, r.repo.GetName(), &github.Reference{
			Ref: github.String("ref/tags/v" + r.newTag.String()),
			Object: &github.GitObject{
				SHA: commit.SHA,
			},
		})
		noError(err)

		pterm.Info.Printfln("Created ref: %v (SHA: %v)", createdRef.GetRef(), createdRef.GetObject().GetSHA())
	}
}
