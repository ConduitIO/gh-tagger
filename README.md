# Github tagger

A set of tools for tagging multiple repositories at once.

## Usage

First build the tools

```
make build
```

Now you have two binaries that you can use together to bump
a bunch of tags in multiple Github repositories.

The commands interact with the Github API so you will need
to provide a [github token](https://github.com/settings/tokens)
in the environment variable `GITHUB_TOKEN`. The token needs
the scope `repo`.

```
# set github token
export GITHUB_TOKEN=my-github-token

# fetch the latest semantic version tag for all repos in
# organization conduitio, bump the minor version and output
# the list of new tags into file repos.txt
./gh-tag-fetcher -bump minor conduitio > repos.txt

# manually edit repos.txt if necessary

# pipe the contents of repos.txt into gh-tag-creator which
# will create the tags according to the list in repos.txt
cat repos.txt | ./gh-tag-creator
```

